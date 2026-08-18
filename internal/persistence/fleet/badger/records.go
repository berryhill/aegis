package badger

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
	badgerdb "github.com/dgraph-io/badger/v4"
)

const (
	familyRegistration         byte = 0x10
	familySource                    = 0x11
	familyAgentRevision             = 0x12
	familyAgentLatest               = 0x13
	familyLoopRevision              = 0x20
	familyLoopValidation            = 0x21
	familyLoopLatest                = 0x22
	familyLoopRequest               = 0x23
	familyLoopProvenance            = 0x24
	familyLoopLifecycleEvent        = 0x25
	familyLoopLifecycleCurrent      = 0x26
	familyGraphRevision             = 0x30
	familyGraphValidation           = 0x31
	familyGraphLatest               = 0x32
	familyGraphRequest              = 0x33
	familySnapshot                  = 0x34
	familySnapshotRequest           = 0x35
	familyGraphLifecycle            = 0x36
	familySubmission                = 0x50
	familySubmissionRequest         = 0x51
	familyRejection                 = 0x52
	familyQueueItem                 = 0x53
	familyQueueTransition           = 0x54
	familyClaim                     = 0x55
	familyClaimByItem               = 0x56
	familyQueueProjection           = 0x57
	familyQueueRetry                = 0x58
	familyQueueCancellation         = 0x59
	familyGraphRun                  = 0x60
	familyLoopExecution             = 0x61
	familyAttempt                   = 0x62
	familyRuntimeArtifact           = 0x63
	familyVerificationReceipt       = 0x64
	familyDisposition               = 0x65
	familyDispositionByRun          = 0x66
	familyAudit                     = 0x40
)

func key(family byte, parts ...string) []byte {
	result := []byte{2, family}
	for _, part := range parts {
		var size [2]byte
		binary.BigEndian.PutUint16(size[:], uint16(len(part)))
		result = append(result, size[:]...)
		result = append(result, part...)
	}
	return result
}

func decodeKeyParts(recordKey []byte, family byte) ([]string, error) {
	if len(recordKey) < 2 || recordKey[0] != 2 || recordKey[1] != family {
		return nil, fleet.ErrCorrupt
	}
	parts := []string{}
	for offset := 2; offset < len(recordKey); {
		if len(recordKey)-offset < 2 {
			return nil, fleet.ErrCorrupt
		}
		size := int(binary.BigEndian.Uint16(recordKey[offset : offset+2]))
		offset += 2
		if size > len(recordKey)-offset {
			return nil, fleet.ErrCorrupt
		}
		parts = append(parts, string(recordKey[offset:offset+size]))
		offset += size
	}
	return parts, nil
}

func revisionPart(revision uint64) string {
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], revision)
	return string(value[:])
}
func sourcePart(source registry.FleetSource) string {
	sum := sha256.Sum256([]byte(source.Key()))
	return hex.EncodeToString(sum[:])
}
func get(txn *badgerdb.Txn, key []byte) ([]byte, error) {
	item, err := txn.Get(key)
	if errors.Is(err, badgerdb.ErrKeyNotFound) {
		return nil, fleet.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return item.ValueCopy(nil)
}
func optional(txn *badgerdb.Txn, key []byte) ([]byte, bool, error) {
	value, err := get(txn, key)
	if errors.Is(err, fleet.ErrNotFound) {
		return nil, false, nil
	}
	return value, err == nil, err
}
func create(txn *badgerdb.Txn, key, value []byte) error {
	if _, found, err := optional(txn, key); err != nil {
		return err
	} else if found {
		return fleet.ErrConflict
	}
	return txn.Set(key, value)
}

func requestBinding(values ...any) ([]byte, error) {
	wire, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(wire)
	return []byte("sha256:" + hex.EncodeToString(digest[:])), nil
}

func (s *Store) RegisterAgent(ctx context.Context, registration registry.AgentRegistration, initial registry.AgentRevision, fact fleet.AuditFact) (created bool, err error) {
	registrationWire, err := registry.MarshalAgentRegistration(registration)
	if err != nil {
		return false, err
	}
	revisionWire, err := registry.MarshalAgentRevision(initial)
	if err != nil {
		return false, err
	}
	if initial.AgentID != registration.AgentID || initial.Revision != 1 || initial.Source != registration.Source || initial.Digest != registration.InitialRevision.Digest {
		return false, errors.New("registration does not bind supplied initial revision")
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		registrationKey := key(familyRegistration, registration.AgentID)
		revisionKey := key(familyAgentRevision, registration.AgentID, revisionPart(1))
		sourceKey := key(familySource, sourcePart(registration.Source))
		if existing, found, loadErr := optional(txn, registrationKey); loadErr != nil {
			return loadErr
		} else if found {
			storedRevision, _, revisionErr := optional(txn, revisionKey)
			if revisionErr == nil && bytes.Equal(existing, registrationWire) && bytes.Equal(storedRevision, revisionWire) {
				created = false
				return nil
			}
			return fleet.ErrConflict
		}
		if _, found, loadErr := optional(txn, sourceKey); loadErr != nil {
			return loadErr
		} else if found {
			return fleet.ErrConflict
		}
		for _, entry := range []struct{ k, v []byte }{{registrationKey, registrationWire}, {sourceKey, []byte(registration.AgentID)}, {revisionKey, revisionWire}, {key(familyAgentLatest, registration.AgentID), []byte(revisionPart(1))}} {
			if setErr := create(txn, entry.k, entry.v); setErr != nil {
				return setErr
			}
		}
		if auditErr := appendAudit(txn, fact); auditErr != nil {
			return auditErr
		}
		created = true
		return nil
	})
	return created, err
}

func (s *Store) PublishAgentRevision(ctx context.Context, revision registry.AgentRevision, fact fleet.AuditFact) error {
	wire, err := registry.MarshalAgentRevision(revision)
	if err != nil {
		return err
	}
	return s.update(ctx, func(txn *badgerdb.Txn) error {
		registrationWire, err := get(txn, key(familyRegistration, revision.AgentID))
		if err != nil {
			return err
		}
		registration, err := registry.UnmarshalAgentRegistration(registrationWire)
		if err != nil {
			return corrupt(err)
		}
		if registration.Source != revision.Source {
			return fleet.ErrConflict
		}
		latest, err := agentLatest(txn, revision.AgentID)
		if err != nil {
			return err
		}
		if latest.Lifecycle == registry.LifecycleRetired {
			return registry.ErrRetired
		}
		if revision.Revision != latest.Revision+1 {
			return fleet.ErrConflict
		}
		if err = create(txn, key(familyAgentRevision, revision.AgentID, revisionPart(revision.Revision)), wire); err != nil {
			return err
		}
		if err = txn.Set(key(familyAgentLatest, revision.AgentID), []byte(revisionPart(revision.Revision))); err != nil {
			return err
		}
		return appendAudit(txn, fact)
	})
}

func agentLatest(txn *badgerdb.Txn, id string) (registry.AgentRevision, error) {
	value, err := get(txn, key(familyAgentLatest, id))
	if err != nil {
		return registry.AgentRevision{}, err
	}
	if len(value) != 8 {
		return registry.AgentRevision{}, corrupt(errors.New("invalid agent latest pointer"))
	}
	return agentRevision(txn, id, binary.BigEndian.Uint64(value))
}
func agentRevision(txn *badgerdb.Txn, id string, revision uint64) (registry.AgentRevision, error) {
	value, err := get(txn, key(familyAgentRevision, id, revisionPart(revision)))
	if err != nil {
		return registry.AgentRevision{}, err
	}
	decoded, err := registry.UnmarshalAgentRevision(value)
	if err != nil || decoded.AgentID != id || decoded.Revision != revision {
		return registry.AgentRevision{}, corrupt(err)
	}
	return decoded, nil
}
func registration(txn *badgerdb.Txn, id string) (registry.AgentRegistration, error) {
	value, err := get(txn, key(familyRegistration, id))
	if err != nil {
		return registry.AgentRegistration{}, err
	}
	decoded, err := registry.UnmarshalAgentRegistration(value)
	if err != nil || decoded.AgentID != id {
		return registry.AgentRegistration{}, corrupt(err)
	}
	return decoded, nil
}
func (s *Store) GetAgentRegistration(ctx context.Context, id string) (out registry.AgentRegistration, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { out, err = registration(txn, id); return err })
	return
}
func (s *Store) GetAgentRegistrationBySource(ctx context.Context, source registry.FleetSource) (out registry.AgentRegistration, err error) {
	if err = source.Validate(); err != nil {
		return
	}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		agentID, e := get(txn, key(familySource, sourcePart(source)))
		if e != nil {
			return e
		}
		out, e = registration(txn, string(agentID))
		if e == nil && out.Source != source {
			return corrupt(errors.New("source index mismatch"))
		}
		return e
	})
	return
}
func (s *Store) GetAgentRevision(ctx context.Context, id string, revision uint64) (out registry.AgentRevision, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { out, err = agentRevision(txn, id, revision); return err })
	return
}
func (s *Store) LatestAgentRevision(ctx context.Context, id string) (out registry.AgentRevision, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { out, err = agentLatest(txn, id); return err })
	return
}
func (s *Store) ListAgentRegistrations(ctx context.Context) (out []registry.AgentRegistration, err error) {
	out = []registry.AgentRegistration{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyRegistration, func(_ []byte, value []byte) error {
			item, e := registry.UnmarshalAgentRegistration(value)
			if e != nil {
				return corrupt(e)
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	return
}

func (s *Store) ListAgentRevisions(ctx context.Context, id string) (out []registry.AgentRevision, err error) {
	out = []registry.AgentRevision{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		if _, e := registration(txn, id); e != nil {
			return e
		}
		return scan(txn, familyAgentRevision, func(recordKey []byte, value []byte) error {
			parts, keyErr := decodeKeyParts(recordKey, familyAgentRevision)
			if keyErr != nil || len(parts) != 2 {
				return fleet.ErrCorrupt
			}
			if parts[0] != id {
				return nil
			}
			item, decodeErr := registry.UnmarshalAgentRevision(value)
			if decodeErr != nil || item.AgentID != id || parts[1] != revisionPart(item.Revision) {
				return corrupt(decodeErr)
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	for index, revision := range out {
		if revision.Revision != uint64(index+1) {
			return nil, fleet.ErrCorrupt
		}
	}
	return
}

func (s *Store) PublishLoop(ctx context.Context, request loop.PublishRequest, fact fleet.AuditFact) (decision loop.PublicationDecision, err error) {
	revisionWire, e := loop.MarshalRevision(request.Revision)
	if e != nil {
		return decision, e
	}
	validationWire, e := loop.MarshalLoopValidationResult(request.Validation)
	if e != nil {
		return decision, e
	}
	provenanceWire, e := loop.MarshalPublicationProvenance(request.Provenance)
	if e != nil {
		return decision, e
	}
	binding, e := requestBinding(revisionWire, validationWire, provenanceWire, request.ExpectedPreviousDigest, request.IdempotencyKey, fact.Event)
	if e != nil {
		return decision, e
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		requestKey := key(familyLoopRequest, request.IdempotencyKey)
		if bound, found, x := optional(txn, requestKey); x != nil {
			return x
		} else if found {
			if bytes.Equal(bound, binding) {
				if verifyErr := verifyLoopPublicationBundle(txn, request, revisionWire, validationWire, provenanceWire); verifyErr != nil {
					return verifyErr
				}
				decision.Idempotent = true
				return nil
			}
			return fleet.ErrConflict
		}
		var previous, existing *loop.LoopRevision
		if request.Revision.Revision > 1 {
			v, x := loopRevision(txn, request.Revision.LoopID, request.Revision.Revision-1)
			if x == nil {
				previous = &v
			} else if !errors.Is(x, fleet.ErrNotFound) {
				return x
			}
		}
		if v, x := loopRevision(txn, request.Revision.LoopID, request.Revision.Revision); x == nil {
			existing = &v
		} else if !errors.Is(x, fleet.ErrNotFound) {
			return x
		}
		if existing != nil {
			return fleet.ErrConflict
		}
		decision, e = loop.ValidatePublication(request, previous, existing)
		if e != nil {
			return e
		}
		if decision.Idempotent {
			return nil
		}
		for _, entry := range []struct{ k, v []byte }{
			{key(familyLoopRevision, request.Revision.LoopID, revisionPart(request.Revision.Revision)), revisionWire},
			{key(familyLoopValidation, request.Revision.LoopID, revisionPart(request.Revision.Revision), request.Validation.Digest), validationWire},
			{key(familyLoopProvenance, request.Revision.LoopID, revisionPart(request.Revision.Revision)), provenanceWire},
			{requestKey, binding},
		} {
			if x := create(txn, entry.k, entry.v); x != nil {
				return x
			}
		}
		if e = txn.Set(key(familyLoopLatest, request.Revision.LoopID), []byte(revisionPart(request.Revision.Revision))); e != nil {
			return e
		}
		return appendAudit(txn, fact)
	})
	return
}
func loopRevision(txn *badgerdb.Txn, id string, revision uint64) (loop.LoopRevision, error) {
	value, err := get(txn, key(familyLoopRevision, id, revisionPart(revision)))
	if err != nil {
		return loop.LoopRevision{}, err
	}
	decoded, err := loop.UnmarshalRevision(value)
	if err != nil || decoded.LoopID != id || decoded.Revision != revision {
		return loop.LoopRevision{}, corrupt(err)
	}
	return decoded, nil
}

func verifyLoopPublicationBundle(txn *badgerdb.Txn, request loop.PublishRequest, revisionWire, validationWire, provenanceWire []byte) error {
	checks := []struct {
		key  []byte
		want []byte
	}{
		{key(familyLoopRevision, request.Revision.LoopID, revisionPart(request.Revision.Revision)), revisionWire},
		{key(familyLoopValidation, request.Revision.LoopID, revisionPart(request.Revision.Revision), request.Validation.Digest), validationWire},
		{key(familyLoopProvenance, request.Revision.LoopID, revisionPart(request.Revision.Revision)), provenanceWire},
	}
	for _, check := range checks {
		stored, err := get(txn, check.key)
		if err != nil || !bytes.Equal(stored, check.want) {
			return fleet.ErrCorrupt
		}
	}
	latest, err := get(txn, key(familyLoopLatest, request.Revision.LoopID))
	if err != nil || len(latest) != 8 || binary.BigEndian.Uint64(latest) < request.Revision.Revision {
		return fleet.ErrCorrupt
	}
	if _, err = loopRevision(txn, request.Revision.LoopID, binary.BigEndian.Uint64(latest)); err != nil {
		return fleet.ErrCorrupt
	}
	return nil
}

func (s *Store) GetLoopRevision(ctx context.Context, id string, revision uint64) (out loop.LoopRevision, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { out, err = loopRevision(txn, id, revision); return err })
	return
}
func (s *Store) GetLoopValidation(ctx context.Context, id string, revision uint64, digest string) (out loop.LoopValidationResult, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		value, e := get(txn, key(familyLoopValidation, id, revisionPart(revision), digest))
		if e != nil {
			return e
		}
		out, e = loop.UnmarshalLoopValidationResult(value)
		if e != nil || out.LoopID != id || out.Revision != revision || out.Digest != digest {
			return corrupt(e)
		}
		return nil
	})
	return
}

func (s *Store) GetLoopPublicationProvenance(ctx context.Context, id string, revision uint64) (out loop.PublicationProvenance, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		value, e := get(txn, key(familyLoopProvenance, id, revisionPart(revision)))
		if e != nil {
			return e
		}
		out, e = loop.UnmarshalPublicationProvenance(value)
		if e != nil || out.Loop.ID != id || out.Loop.Revision != revision {
			return corrupt(e)
		}
		return nil
	})
	return
}

func (s *Store) ListLoopRevisions(ctx context.Context) (out []loop.LoopRevision, err error) {
	out = []loop.LoopRevision{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyLoopRevision, func(recordKey []byte, value []byte) error {
			item, decodeErr := loop.UnmarshalRevision(value)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyLoopRevision)
			if keyErr != nil || len(parts) != 2 || parts[0] != item.LoopID || parts[1] != revisionPart(item.Revision) {
				return fleet.ErrCorrupt
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].LoopID == out[j].LoopID {
			return out[i].Revision < out[j].Revision
		}
		return out[i].LoopID < out[j].LoopID
	})
	return
}

func (s *Store) ListLoopValidations(ctx context.Context) (out []loop.LoopValidationResult, err error) {
	out = []loop.LoopValidationResult{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyLoopValidation, func(recordKey []byte, value []byte) error {
			item, decodeErr := loop.UnmarshalLoopValidationResult(value)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyLoopValidation)
			if keyErr != nil || len(parts) != 3 || parts[0] != item.LoopID || parts[1] != revisionPart(item.Revision) || parts[2] != item.Digest {
				return fleet.ErrCorrupt
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].LoopID == out[j].LoopID {
			if out[i].Revision == out[j].Revision {
				return out[i].Digest < out[j].Digest
			}
			return out[i].Revision < out[j].Revision
		}
		return out[i].LoopID < out[j].LoopID
	})
	return
}

func (s *Store) ListLoopPublicationProvenance(ctx context.Context) (out []loop.PublicationProvenance, err error) {
	out = []loop.PublicationProvenance{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyLoopProvenance, func(recordKey, value []byte) error {
			item, decodeErr := loop.UnmarshalPublicationProvenance(value)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyLoopProvenance)
			if keyErr != nil || len(parts) != 2 || parts[0] != item.Loop.ID || parts[1] != revisionPart(item.Loop.Revision) {
				return fleet.ErrCorrupt
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Loop.ID == out[j].Loop.ID {
			return out[i].Loop.Revision < out[j].Loop.Revision
		}
		return out[i].Loop.ID < out[j].Loop.ID
	})
	return
}

func (s *Store) AppendLoopLifecycle(ctx context.Context, request loop.LifecycleRequest, fact fleet.AuditFact) (event loop.LifecycleEvent, idempotent bool, err error) {
	event = request.Event
	wire, err := loop.MarshalLifecycleEvent(event)
	if err != nil {
		return loop.LifecycleEvent{}, false, err
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		events, loadErr := loopLifecycleEvents(txn, event.LoopID)
		if loadErr != nil {
			return loadErr
		}
		currentID, hasCurrent, loadErr := optional(txn, key(familyLoopLifecycleCurrent, event.LoopID))
		if loadErr != nil {
			return loadErr
		}
		chain, loadErr := orderLoopLifecycle(events, string(currentID), hasCurrent)
		if loadErr != nil {
			return loadErr
		}
		if loadErr = validateLoopLifecycleChain(txn, chain); loadErr != nil {
			return loadErr
		}

		eventKey := key(familyLoopLifecycleEvent, event.LoopID, event.EventID)
		if existing, found, loadErr := optional(txn, eventKey); loadErr != nil {
			return loadErr
		} else if found {
			stored, decodeErr := loop.UnmarshalLifecycleEvent(existing)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			// OccurredAt and Digest are assigned at the authenticated
			// application boundary. A retry of the same lifecycle intent may
			// therefore carry a later timestamp while still being the same
			// idempotent request. Return the original immutable event.
			if !sameLifecycleIntent(stored, event) {
				return fleet.ErrConflict
			}
			event = stored
			idempotent = true
			return nil
		}

		current := loop.Lifecycle{LoopID: event.LoopID, State: loop.LifecycleDraft}
		previousDigest := ""
		if len(chain) > 0 {
			previous := chain[len(chain)-1]
			previousDigest = previous.Digest
			if previous.State == loop.LifecycleActive {
				current = loop.Lifecycle{LoopID: event.LoopID, State: loop.LifecycleActive, ActiveRevision: previous.Revision.Revision, ActiveDigest: previous.Revision.Digest}
			} else {
				current = loop.Lifecycle{LoopID: event.LoopID, State: loop.LifecycleRetired}
			}
		}
		if event.PreviousDigest != previousDigest || request.ExpectedPreviousDigest != previousDigest {
			return fleet.ErrConflict
		}
		if event.State == loop.LifecycleActive {
			revision, loadErr := loopRevision(txn, event.LoopID, event.Revision.Revision)
			if loadErr != nil || revision.Digest != event.Revision.Digest {
				return fleet.ErrConflict
			}
			if _, transitionErr := loop.Activate(current, revision); transitionErr != nil {
				return transitionErr
			}
		} else if _, transitionErr := loop.Retire(current); transitionErr != nil {
			return transitionErr
		}
		if createErr := create(txn, eventKey, wire); createErr != nil {
			return createErr
		}
		if setErr := txn.Set(key(familyLoopLifecycleCurrent, event.LoopID), []byte(event.EventID)); setErr != nil {
			return setErr
		}
		return appendAudit(txn, fact)
	})
	return
}

func sameLifecycleIntent(left, right loop.LifecycleEvent) bool {
	left.OccurredAt, right.OccurredAt = time.Time{}, time.Time{}
	left.Digest, right.Digest = "", ""
	return left == right
}

func loopLifecycleEvents(txn *badgerdb.Txn, loopID string) ([]loop.LifecycleEvent, error) {
	events := []loop.LifecycleEvent{}
	err := scan(txn, familyLoopLifecycleEvent, func(recordKey, value []byte) error {
		item, decodeErr := loop.UnmarshalLifecycleEvent(value)
		if decodeErr != nil {
			return corrupt(decodeErr)
		}
		parts, keyErr := decodeKeyParts(recordKey, familyLoopLifecycleEvent)
		if keyErr != nil || len(parts) != 2 || parts[0] != item.LoopID || parts[1] != item.EventID {
			return fleet.ErrCorrupt
		}
		if item.LoopID == loopID {
			events = append(events, item)
		}
		return nil
	})
	return events, err
}

func orderLoopLifecycle(events []loop.LifecycleEvent, currentID string, hasCurrent bool) ([]loop.LifecycleEvent, error) {
	if len(events) == 0 {
		if hasCurrent {
			return nil, fleet.ErrCorrupt
		}
		return []loop.LifecycleEvent{}, nil
	}
	if !hasCurrent {
		return nil, fleet.ErrCorrupt
	}
	byDigest := make(map[string]loop.LifecycleEvent, len(events))
	children := make(map[string]loop.LifecycleEvent, len(events)-1)
	var genesis loop.LifecycleEvent
	genesisCount := 0
	for _, event := range events {
		if _, exists := byDigest[event.Digest]; exists {
			return nil, fleet.ErrCorrupt
		}
		byDigest[event.Digest] = event
		if event.PreviousDigest == "" {
			genesis = event
			genesisCount++
			continue
		}
		if _, exists := children[event.PreviousDigest]; exists {
			return nil, fleet.ErrCorrupt
		}
		children[event.PreviousDigest] = event
	}
	if genesisCount != 1 {
		return nil, fleet.ErrCorrupt
	}
	ordered := make([]loop.LifecycleEvent, 0, len(events))
	visited := make(map[string]struct{}, len(events))
	current := genesis
	for {
		if _, exists := visited[current.Digest]; exists {
			return nil, fleet.ErrCorrupt
		}
		visited[current.Digest] = struct{}{}
		ordered = append(ordered, current)
		next, exists := children[current.Digest]
		if !exists {
			break
		}
		current = next
	}
	if len(ordered) != len(events) || ordered[len(ordered)-1].EventID != currentID {
		return nil, fleet.ErrCorrupt
	}
	return ordered, nil
}

func validateLoopLifecycleChain(txn *badgerdb.Txn, chain []loop.LifecycleEvent) error {
	if len(chain) == 0 {
		return nil
	}
	current := loop.Lifecycle{LoopID: chain[0].LoopID, State: loop.LifecycleDraft}
	for _, event := range chain {
		if event.LoopID != current.LoopID {
			return fleet.ErrCorrupt
		}
		if event.State == loop.LifecycleActive {
			revision, err := loopRevision(txn, event.LoopID, event.Revision.Revision)
			if err != nil || revision.Digest != event.Revision.Digest {
				return fleet.ErrCorrupt
			}
			current, err = loop.Activate(current, revision)
			if err != nil {
				return fleet.ErrCorrupt
			}
			continue
		}
		var err error
		current, err = loop.Retire(current)
		if err != nil {
			return fleet.ErrCorrupt
		}
	}
	return nil
}

func (s *Store) ListLoopLifecycleEvents(ctx context.Context) (out []loop.LifecycleEvent, err error) {
	out = []loop.LifecycleEvent{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		byLoop := map[string][]loop.LifecycleEvent{}
		if scanErr := scan(txn, familyLoopLifecycleEvent, func(recordKey, value []byte) error {
			item, decodeErr := loop.UnmarshalLifecycleEvent(value)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyLoopLifecycleEvent)
			if keyErr != nil || len(parts) != 2 || parts[0] != item.LoopID || parts[1] != item.EventID {
				return fleet.ErrCorrupt
			}
			byLoop[item.LoopID] = append(byLoop[item.LoopID], item)
			return nil
		}); scanErr != nil {
			return scanErr
		}
		current := map[string]string{}
		if scanErr := scan(txn, familyLoopLifecycleCurrent, func(recordKey, value []byte) error {
			parts, keyErr := decodeKeyParts(recordKey, familyLoopLifecycleCurrent)
			if keyErr != nil || len(parts) != 1 || len(value) == 0 {
				return fleet.ErrCorrupt
			}
			current[parts[0]] = string(value)
			return nil
		}); scanErr != nil {
			return scanErr
		}
		ids := make([]string, 0, len(byLoop))
		for id := range byLoop {
			ids = append(ids, id)
		}
		for id := range current {
			if _, exists := byLoop[id]; !exists {
				return fleet.ErrCorrupt
			}
		}
		sort.Strings(ids)
		for _, id := range ids {
			ordered, orderErr := orderLoopLifecycle(byLoop[id], current[id], current[id] != "")
			if orderErr != nil {
				return orderErr
			}
			if orderErr = validateLoopLifecycleChain(txn, ordered); orderErr != nil {
				return orderErr
			}
			out = append(out, ordered...)
		}
		return nil
	})
	return
}

func (s *Store) PublishGraph(ctx context.Context, request graph.PublishRequest, fact fleet.AuditFact) (decision graph.PublicationDecision, err error) {
	revisionWire, e := graph.MarshalRevision(request.Revision)
	if e != nil {
		return decision, e
	}
	validationWire, e := graph.MarshalValidationResult(request.Validation)
	if e != nil {
		return decision, e
	}
	binding, e := requestBinding(revisionWire, validationWire, request.ExpectedPreviousDigest, request.IdempotencyKey, fact.Event)
	if e != nil {
		return decision, e
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		requestKey := key(familyGraphRequest, request.IdempotencyKey)
		if bound, found, x := optional(txn, requestKey); x != nil {
			return x
		} else if found {
			if bytes.Equal(bound, binding) {
				decision.Idempotent = true
				return nil
			}
			return fleet.ErrConflict
		}
		var current, existing *graph.GraphRevision
		if request.Revision.Revision > 1 {
			v, x := graphRevision(txn, request.Revision.GraphID, request.Revision.Revision-1)
			if x == nil {
				current = &v
			} else if !errors.Is(x, fleet.ErrNotFound) {
				return x
			}
		}
		if v, x := graphRevision(txn, request.Revision.GraphID, request.Revision.Revision); x == nil {
			existing = &v
		} else if !errors.Is(x, fleet.ErrNotFound) {
			return x
		}
		decision, e = graph.ValidatePublication(request, current, existing)
		if e != nil {
			return e
		}
		if decision.Idempotent {
			return nil
		}
		for _, entry := range []struct{ k, v []byte }{{key(familyGraphRevision, request.Revision.GraphID, revisionPart(request.Revision.Revision)), revisionWire}, {key(familyGraphValidation, request.Revision.GraphID, revisionPart(request.Revision.Revision), request.Validation.Digest), validationWire}, {requestKey, binding}} {
			if x := create(txn, entry.k, entry.v); x != nil {
				return x
			}
		}
		lifecycleWire, x := json.Marshal(graph.Lifecycle{GraphID: request.Revision.GraphID, State: graph.LifecycleActive, ActiveRevision: request.Revision.Revision, ActiveDigest: request.Revision.Digest})
		if x != nil {
			return x
		}
		if x = txn.Set(key(familyGraphLifecycle, request.Revision.GraphID), lifecycleWire); x != nil {
			return x
		}
		if e = txn.Set(key(familyGraphLatest, request.Revision.GraphID), []byte(revisionPart(request.Revision.Revision))); e != nil {
			return e
		}
		return appendAudit(txn, fact)
	})
	return
}
func graphRevision(txn *badgerdb.Txn, id string, revision uint64) (graph.GraphRevision, error) {
	value, err := get(txn, key(familyGraphRevision, id, revisionPart(revision)))
	if err != nil {
		return graph.GraphRevision{}, err
	}
	decoded, err := graph.UnmarshalRevision(value)
	if err != nil || decoded.GraphID != id || decoded.Revision != revision {
		return graph.GraphRevision{}, corrupt(err)
	}
	return decoded, nil
}
func (s *Store) GetGraphRevision(ctx context.Context, id string, revision uint64) (out graph.GraphRevision, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error { out, err = graphRevision(txn, id, revision); return err })
	return
}
func (s *Store) GetGraphValidation(ctx context.Context, id string, revision uint64, digest string) (out graph.GraphValidationResult, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		value, e := get(txn, key(familyGraphValidation, id, revisionPart(revision), digest))
		if e != nil {
			return e
		}
		out, e = graph.UnmarshalValidationResult(value)
		if e != nil || out.GraphID != id || out.Revision != revision || out.Digest != digest {
			return corrupt(e)
		}
		return nil
	})
	return
}
func (s *Store) ListGraphRevisions(ctx context.Context) (out []graph.GraphRevision, err error) {
	out = []graph.GraphRevision{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyGraphRevision, func(recordKey []byte, value []byte) error {
			item, decodeErr := graph.UnmarshalRevision(value)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyGraphRevision)
			if keyErr != nil || len(parts) != 2 || parts[0] != item.GraphID || parts[1] != revisionPart(item.Revision) {
				return fleet.ErrCorrupt
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].GraphID == out[j].GraphID {
			return out[i].Revision < out[j].Revision
		}
		return out[i].GraphID < out[j].GraphID
	})
	return
}

func (s *Store) ListGraphValidations(ctx context.Context) (out []graph.GraphValidationResult, err error) {
	out = []graph.GraphValidationResult{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyGraphValidation, func(recordKey []byte, value []byte) error {
			item, decodeErr := graph.UnmarshalValidationResult(value)
			if decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyGraphValidation)
			if keyErr != nil || len(parts) != 3 || parts[0] != item.GraphID || parts[1] != revisionPart(item.Revision) || parts[2] != item.Digest {
				return fleet.ErrCorrupt
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].GraphID == out[j].GraphID {
			if out[i].Revision == out[j].Revision {
				return out[i].Digest < out[j].Digest
			}
			return out[i].Revision < out[j].Revision
		}
		return out[i].GraphID < out[j].GraphID
	})
	return
}

func (s *Store) GetGraphLifecycle(ctx context.Context, id string) (out graph.Lifecycle, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		value, loadErr := get(txn, key(familyGraphLifecycle, id))
		if loadErr != nil {
			return loadErr
		}
		if decodeErr := json.Unmarshal(value, &out); decodeErr != nil || out.GraphID != id || out.State != graph.LifecycleActive || out.ActiveRevision == 0 || out.ActiveDigest == "" {
			return corrupt(decodeErr)
		}
		revision, loadErr := graphRevision(txn, id, out.ActiveRevision)
		if loadErr != nil || revision.Digest != out.ActiveDigest {
			return corrupt(loadErr)
		}
		return nil
	})
	return
}

func (s *Store) ListGraphLifecycles(ctx context.Context) (out []graph.Lifecycle, err error) {
	out = []graph.Lifecycle{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyGraphLifecycle, func(recordKey, value []byte) error {
			var item graph.Lifecycle
			if decodeErr := json.Unmarshal(value, &item); decodeErr != nil {
				return corrupt(decodeErr)
			}
			parts, keyErr := decodeKeyParts(recordKey, familyGraphLifecycle)
			if keyErr != nil || len(parts) != 1 || parts[0] != item.GraphID || item.State != graph.LifecycleActive || item.ActiveRevision == 0 || item.ActiveDigest == "" {
				return fleet.ErrCorrupt
			}
			revision, loadErr := graphRevision(txn, item.GraphID, item.ActiveRevision)
			if loadErr != nil || revision.Digest != item.ActiveDigest {
				return corrupt(loadErr)
			}
			out = append(out, item)
			return nil
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].GraphID < out[j].GraphID })
	return
}

func (s *Store) CreateGraphRunSnapshot(ctx context.Context, snapshot graph.GraphRunSnapshot, fact fleet.AuditFact) (created bool, err error) {
	wire, e := graph.MarshalRunSnapshot(snapshot)
	if e != nil {
		return false, e
	}
	binding, e := requestBinding(wire, fact.Event)
	if e != nil {
		return false, e
	}
	err = s.update(ctx, func(txn *badgerdb.Txn) error {
		snapshotKey := key(familySnapshot, snapshot.SnapshotID)
		requestKey := key(familySnapshotRequest, snapshot.SnapshotID)
		if value, found, x := optional(txn, snapshotKey); x != nil {
			return x
		} else if found {
			bound, boundFound, loadErr := optional(txn, requestKey)
			if loadErr != nil {
				return loadErr
			}
			if bytes.Equal(value, wire) && boundFound && bytes.Equal(bound, binding) {
				return nil
			}
			return fleet.ErrConflict
		}
		if x := verifySnapshotReferences(txn, snapshot); x != nil {
			return x
		}
		if x := create(txn, snapshotKey, wire); x != nil {
			return x
		}
		if x := create(txn, requestKey, binding); x != nil {
			return x
		}
		if x := appendAudit(txn, fact); x != nil {
			return x
		}
		created = true
		return nil
	})
	return
}
func verifySnapshotReferences(txn *badgerdb.Txn, snapshot graph.GraphRunSnapshot) error {
	revision, err := graphRevision(txn, snapshot.Graph.ID, snapshot.Graph.Revision)
	if err != nil {
		return err
	}
	if revision.Digest != snapshot.Graph.Digest {
		return fleet.ErrConflict
	}
	expectedSnapshot, err := graph.NewRunSnapshot(snapshot.SnapshotID, revision, snapshot.Inputs)
	if err != nil || expectedSnapshot.Digest != snapshot.Digest {
		return fleet.ErrConflict
	}
	if err = requireGraphValidation(txn, revision); err != nil {
		return err
	}

	for _, participant := range snapshot.Participants {
		stored, loadErr := agentRevision(txn, participant.ID, participant.Revision)
		if loadErr != nil {
			return loadErr
		}
		if stored.Digest != participant.Digest || stored.Lifecycle != registry.LifecycleEnabled {
			return fleet.ErrConflict
		}
	}
	for _, loopRef := range snapshot.Loops {
		stored, loadErr := loopRevision(txn, loopRef.ID, loopRef.Revision)
		if loadErr != nil {
			return loadErr
		}
		if stored.Digest != loopRef.Digest {
			return fleet.ErrConflict
		}
		if loadErr = requireLoopValidation(txn, stored); loadErr != nil {
			return loadErr
		}
	}
	return nil
}

func requireGraphValidation(txn *badgerdb.Txn, revision graph.GraphRevision) error {
	prefix := key(familyGraphValidation, revision.GraphID, revisionPart(revision.Revision))
	return requireValidation(txn, prefix, func(value []byte) (bool, error) {
		result, err := graph.UnmarshalValidationResult(value)
		if err != nil {
			return false, corrupt(err)
		}
		return result.GraphID == revision.GraphID && result.Revision == revision.Revision && result.RevisionDigest == revision.Digest && result.Validator == revision.Validator && result.Outcome == graph.ValidationValid, nil
	})
}

func requireLoopValidation(txn *badgerdb.Txn, revision loop.LoopRevision) error {
	prefix := key(familyLoopValidation, revision.LoopID, revisionPart(revision.Revision))
	return requireValidation(txn, prefix, func(value []byte) (bool, error) {
		result, err := loop.UnmarshalLoopValidationResult(value)
		if err != nil {
			return false, corrupt(err)
		}
		return result.LoopID == revision.LoopID && result.Revision == revision.Revision && result.RevisionDigest == revision.Digest && result.Validator == revision.Validator && result.Outcome == loop.ValidationValid, nil
	})
}

func requireValidation(txn *badgerdb.Txn, prefix []byte, matches func([]byte) (bool, error)) error {
	iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer iterator.Close()
	found := false
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		value, err := iterator.Item().ValueCopy(nil)
		if err != nil {
			return err
		}
		match, err := matches(value)
		if err != nil {
			return err
		}
		if match {
			found = true
		}
	}
	if !found {
		return fleet.ErrNotFound
	}
	return nil
}

func (s *Store) GetGraphRunSnapshot(ctx context.Context, id string) (out graph.GraphRunSnapshot, err error) {
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		value, e := get(txn, key(familySnapshot, id))
		if e != nil {
			return e
		}
		out, e = graph.UnmarshalRunSnapshot(value)
		if e != nil || out.SnapshotID != id {
			return corrupt(e)
		}
		return nil
	})
	return
}

func appendAudit(txn *badgerdb.Txn, fact fleet.AuditFact) error {
	event := fact.Event
	if event.ID != "" || !event.OccurredAt.IsZero() || event.PreviousDigest != "" || event.EventDigest != "" {
		return errors.New("audit chain fields are repository-assigned")
	}
	if event.Type == "" || event.Outcome == "" || event.Reason == "" {
		return errors.New("authoritative audit type, outcome, and reason are required")
	}
	if len(event.Metadata) > 64 {
		return errors.New("audit metadata exceeds bounded limit")
	}
	for k, v := range event.Metadata {
		if k == "" || len(k) > 128 || len(v) > 1024 {
			return errors.New("audit metadata is malformed")
		}
	}
	var sequence uint64
	var previous string
	prefix := key(familyAudit)
	iterator := txn.NewIterator(badgerdb.IteratorOptions{PrefetchValues: true, Reverse: true})
	defer iterator.Close()
	iterator.Rewind()
	for ; iterator.Valid(); iterator.Next() {
		item := iterator.Item()
		if !bytes.HasPrefix(item.Key(), prefix) {
			continue
		}
		value, e := item.ValueCopy(nil)
		if e != nil {
			return e
		}
		var prior core.AuditEvent
		if e = json.Unmarshal(value, &prior); e != nil {
			return corrupt(e)
		}
		sequence = parseAuditSequence(item.Key())
		previous = prior.EventDigest
		break
	}
	sequence++
	identifier := make([]byte, 16)
	if _, e := rand.Read(identifier); e != nil {
		return e
	}
	event.ID = "evt-" + hex.EncodeToString(identifier)
	event.OccurredAt = time.Now().UTC()
	event.PreviousDigest = previous
	event.EventDigest = core.Digest(event)
	wire, e := json.Marshal(event)
	if e != nil {
		return e
	}
	return create(txn, key(familyAudit, revisionPart(sequence)), wire)
}
func parseAuditSequence(encoded []byte) uint64 {
	if len(encoded) < 12 {
		return 0
	}
	return binary.BigEndian.Uint64(encoded[len(encoded)-8:])
}
func (s *Store) AuditEvents(ctx context.Context) (events []core.AuditEvent, err error) {
	events = []core.AuditEvent{}
	err = s.view(ctx, func(txn *badgerdb.Txn) error {
		return scan(txn, familyAudit, func(_ []byte, value []byte) error {
			var event core.AuditEvent
			if e := json.Unmarshal(value, &event); e != nil {
				return corrupt(e)
			}
			copyEvent := event
			copyEvent.EventDigest = ""
			if core.Digest(copyEvent) != event.EventDigest {
				return corrupt(errors.New("audit digest mismatch"))
			}
			if len(events) == 0 {
				if event.PreviousDigest != "" {
					return corrupt(errors.New("audit genesis mismatch"))
				}
			} else if event.PreviousDigest != events[len(events)-1].EventDigest {
				return corrupt(errors.New("audit chain mismatch"))
			}
			events = append(events, event)
			return nil
		})
	})
	return
}
func scan(txn *badgerdb.Txn, family byte, visit func([]byte, []byte) error) error {
	prefix := []byte{2, family}
	iterator := txn.NewIterator(badgerdb.DefaultIteratorOptions)
	defer iterator.Close()
	for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
		item := iterator.Item()
		value, e := item.ValueCopy(nil)
		if e != nil {
			return e
		}
		if e = visit(item.KeyCopy(nil), value); e != nil {
			return e
		}
	}
	return nil
}
func corrupt(err error) error {
	if err == nil {
		err = errors.New("identity mismatch")
	}
	return fmt.Errorf("%w: %v", fleet.ErrCorrupt, err)
}
