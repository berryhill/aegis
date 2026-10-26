package badger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/registry"
	badgerdb "github.com/dgraph-io/badger/v4"
)

func recoveryCanonical[T any](b []byte, decode func([]byte) (T, error)) error {
	v, e := decode(b)
	if e != nil {
		return e
	}
	wire, e := json.Marshal(v)
	if e != nil || !bytes.Equal(wire, b) {
		return fleet.ErrCorrupt
	}
	return nil
}
func recoveryJSON[T any](b []byte) error {
	var v T
	if json.Unmarshal(b, &v) != nil {
		return fleet.ErrCorrupt
	}
	wire, e := json.Marshal(v)
	if e != nil || !bytes.Equal(wire, b) {
		return fleet.ErrCorrupt
	}
	return nil
}

func verifyFleetRecovery(ctx context.Context, db *badgerdb.DB) (string, uint64, error) {
	h := sha256.New()
	var count uint64
	schema := false
	s := &Store{db: db}
	err := db.View(func(tx *badgerdb.Txn) error {
		it := tx.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			if e := ctx.Err(); e != nil {
				return e
			}
			count++
			if count > 1000000 {
				return errors.New("fleet recovery record bound")
			}
			k := it.Item().KeyCopy(nil)
			b, e := it.Item().ValueCopy(nil)
			if e != nil {
				return e
			}
			var size [8]byte
			for _, v := range [][]byte{k, b} {
				binary.BigEndian.PutUint64(size[:], uint64(len(v)))
				h.Write(size[:])
				h.Write(v)
			}
			if bytes.Equal(k, []byte("\x01schema")) {
				if string(b) != schemaVersion {
					return fleet.ErrCorrupt
				}
				schema = true
				continue
			}
			if implementationKey(k) {
				if e = implementation.VerifyRecoveryRecord(s.ImplementationStore(), k, b); e != nil {
					return e
				}
				continue
			}
			if len(k) < 2 || k[0] != 2 {
				return fmt.Errorf("unknown fleet recovery key %x", k)
			}
			p, e := decodeKeyParts(k, k[1])
			if e != nil || len(p) == 0 {
				return fleet.ErrCorrupt
			}
			for _, v := range p {
				if v == "" {
					return fleet.ErrCorrupt
				}
			}
			// Bind every record to its encoded key, including otherwise hidden orphans.
			switch k[1] {
			case familyRegistration:
				var v registry.AgentRegistration
				v, e = registry.UnmarshalAgentRegistration(b)
				if e == nil && (len(p) != 1 || v.AgentID != p[0]) {
					e = fleet.ErrCorrupt
				}
				if e == nil {
					_, e = agentRevision(tx, v.AgentID, 1)
				}
			case familySource:
				var v registry.AgentRegistration
				v, e = registration(tx, string(b))
				if e == nil && (len(p) != 1 || sourcePart(v.Source) != p[0]) {
					e = fleet.ErrCorrupt
				}
			case familyAgentRevision:
				if len(p) != 2 || len(p[1]) != 8 {
					return fleet.ErrCorrupt
				}
				_, e = agentRevision(tx, p[0], binary.BigEndian.Uint64([]byte(p[1])))
			case familyAgentLatest:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				_, e = agentLatest(tx, p[0])
			case familyLoopRevision:
				if len(p) != 2 || len(p[1]) != 8 {
					return fleet.ErrCorrupt
				}
				var v loop.LoopRevision
				v, e = loopRevision(tx, p[0], binary.BigEndian.Uint64([]byte(p[1])))
				if e == nil {
					e = requireLoopValidation(tx, v)
				}
			case familyLoopValidation:
				e = recoveryCanonical(b, loop.UnmarshalLoopValidationResult)
			case familyLoopProvenance:
				e = recoveryCanonical(b, loop.UnmarshalPublicationProvenance)
			case familyLoopLatest:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				_, e = availabilityLatest(tx, p[0])
			case familyLoopLifecycleEvent:
				e = recoveryCanonical(b, loop.UnmarshalLifecycleEvent)
			case familyLoopLifecycleCurrent:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				_, e = get(tx, key(familyLoopLifecycleEvent, p[0], string(b)))
			case familyLoopAvailabilityEvent:
				_, e = decodeAvailabilityRecord(b)
			case familyLoopAvailabilityHead:
				e = recoveryCanonical(b, loop.UnmarshalAvailabilityEvent)
			case familyGraphRevision:
				if len(p) != 2 || len(p[1]) != 8 {
					return fleet.ErrCorrupt
				}
				var v graph.GraphRevision
				v, e = graphRevision(tx, p[0], binary.BigEndian.Uint64([]byte(p[1])))
				if e == nil {
					e = requireGraphValidation(tx, v)
				}
			case familyGraphValidation:
				e = recoveryCanonical(b, graph.UnmarshalValidationResult)
			case familyGraphLatest:
				if len(p) != 1 || len(b) != 8 {
					return fleet.ErrCorrupt
				}
				_, e = graphRevision(tx, p[0], binary.BigEndian.Uint64(b))
			case familyGraphLifecycle:
				e = recoveryJSON[graph.Lifecycle](b)
			case familySnapshot:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				var v graph.GraphRunSnapshot
				v, e = loadSnapshot(tx, p[0])
				if e == nil {
					e = verifySnapshotReferences(tx, v)
				}
			case familySubmission:
				e = recoveryCanonical(b, queue.UnmarshalSubmission)
			case familyRejection:
				e = recoveryCanonical(b, queue.UnmarshalRejection)
			case familyQueueItem:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				var v queue.Item
				v, e = loadQueueItem(tx, p[0])
				if e == nil {
					var wire []byte
					wire, e = get(tx, key(familySubmission, v.Submission.ID))
					if e == nil {
						var sub queue.Submission
						sub, e = queue.UnmarshalSubmission(wire)
						if e == nil && (sub.Digest != v.Submission.Digest || sub.Snapshot != v.Snapshot) {
							e = fleet.ErrCorrupt
						}
					}
				}
			case familyQueueProjection:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				var v queue.Projection
				v, e = loadQueueProjection(tx, p[0])
				if e == nil {
					e = validateProjectionBasis(tx, v)
				}
			case familyQueueTransition:
				e = recoveryCanonical(b, queue.UnmarshalTransition)
			case familyClaim:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				_, e = loadClaim(tx, p[0])
			case familyClaimByItem:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				var v queue.Claim
				v, e = loadClaim(tx, string(b))
				if e == nil && v.QueueItem.ID != p[0] {
					e = fleet.ErrCorrupt
				}
			case familyQueueRetry:
				e = recoveryCanonical(b, queue.UnmarshalRetry)
			case familyQueueCancellation:
				e = recoveryCanonical(b, queue.UnmarshalCancellation)
			case familyQueueRuntimeBinding:
				e = recoveryCanonical(b, queue.UnmarshalRuntimeBinding)
			case familyGraphRun:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				_, e = loadGraphRun(tx, p[0])
			case familyLoopExecution:
				if len(p) == 2 && p[1] == "binding" {
					e = recoveryBinding(b)
				} else if len(p) == 1 {
					_, e = loadLoopExecution(tx, p[0])
				} else {
					e = fleet.ErrCorrupt
				}
			case familyAttempt:
				e = recoveryCanonical(b, execution.UnmarshalAttempt)
			case familyRuntimeArtifact:
				e = recoveryJSON[evidence.RuntimeArtifact](b)
			case familyVerificationReceipt:
				e = recoveryJSON[evidence.VerificationReceipt](b)
			case familyDisposition:
				e = recoveryCanonical(b, disposition.Unmarshal)
			case familyDispositionByRun:
				if len(p) != 1 {
					return fleet.ErrCorrupt
				}
				var wire []byte
				wire, e = get(tx, key(familyDisposition, string(b)))
				if e == nil {
					var v disposition.Record
					v, e = disposition.Unmarshal(wire)
					if e == nil && v.GraphRunID != p[0] {
						e = fleet.ErrCorrupt
					}
				}
			case familyLoopRequest, familyGraphRequest, familySnapshotRequest, familySubmissionRequest:
				e = recoveryBinding(b)
			case familyAudit:
				if len(p) != 1 || len(p[0]) != 8 {
					return fleet.ErrCorrupt
				}
				e = recoveryJSON[core.AuditEvent](b)
			case familyContribution:
				if len(p) == 1 {
					var c contribution.Contract
					c, e = loadContribution(tx, p[0])
					if e == nil {
						var item queue.Item
						item, e = loadQueueItem(tx, c.QueueItemID)
						if e == nil && (item.GraphRunID != c.RunID || item.Submission.ID != c.SubmissionID) {
							e = fleet.ErrCorrupt
						}
					}
				} else {
					e = recoveryCanonical(b, queue.UnmarshalArchive)
				}
			case familyContributionRequest:
				if len(p) == 2 {
					var c contribution.Contract
					c, e = loadContribution(tx, string(b))
					if e == nil && (c.OwnerID != p[0] || c.Key != p[1]) {
						e = recoveryCanonical(b, queue.UnmarshalArchive)
					}
				} else {
					e = fleet.ErrCorrupt
				}
			case familyContributionIssue:
				if len(p) != 2 {
					return fleet.ErrCorrupt
				}
				var c contribution.Contract
				c, e = loadContribution(tx, string(b))
				if e == nil && !bytes.Equal(k, issueKey(c)) {
					e = fleet.ErrCorrupt
				}
			case familyContributionControl:
				if len(p) == 2 {
					_, e = loadContributionControl(tx, p[0])
				} else {
					e = recoveryCanonical(b, queue.UnmarshalPreferences)
				}
			case familyQueuePreferencesHead:
				e = recoveryCanonical(b, queue.UnmarshalPreferences)
			case familyContributionContinuity:
				if len(p) != 2 {
					return fleet.ErrCorrupt
				}
				_, _, e = loadContributionContinuity(tx, p[0])
			case familyContributionDecision:
				var d contribution.Decision
				e = json.Unmarshal(b, &d)
				if e == nil {
					wire, _ := json.Marshal(d)
					if !bytes.Equal(wire, b) || len(p) != 2 || d.QueueItemID != p[0] || d.ID != p[1] {
						e = fleet.ErrCorrupt
					}
					if e == nil {
						var c contribution.Contract
						c, e = loadContribution(tx, p[0])
						if e == nil && (d.ContractDigest != c.Digest || d.OwnerID != c.OwnerID || d.Key != c.Key) {
							e = fleet.ErrCorrupt
						}
					}
				}
			default:
				return fmt.Errorf("unknown fleet family %x", k[1])
			}
			if e != nil {
				return fmt.Errorf("fleet recovery key %x: %w", k, e)
			}
		}
		return nil
	})
	if err == nil && !schema {
		err = fleet.ErrCorrupt
	}
	if err == nil {
		_, err = s.AuditEvents(ctx)
	}
	return hex.EncodeToString(h.Sum(nil)), count, err
}
func recoveryBinding(b []byte) error {
	if len(b) != 71 || !strings.HasPrefix(string(b), "sha256:") {
		return fleet.ErrCorrupt
	}
	_, e := hex.DecodeString(string(b[7:]))
	return e
}
