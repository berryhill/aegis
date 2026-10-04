package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

const (
	DoerDraftTTL         = 7 * 24 * time.Hour
	DoerDraftMaxRetained = 16
	DoerDraftMaxBytes    = 64 * 1024
	doerDraftKind        = "doer-drafts"
)

var (
	ErrDoerDraftInvalid  = errors.New("invalid Doer draft")
	ErrDoerDraftCapacity = errors.New("Doer draft retention capacity exhausted")
	doerDraftIDPattern   = regexp.MustCompile(`^doerdraft-[0-9a-f]{32}$`)
	doerDraftKeyPattern  = regexp.MustCompile(`^doerpub-[0-9a-f]{32}$`)
)

// DoerDraftInput is proposal data only. Zero ID/version creates a draft;
// otherwise ExpectedVersion is a compare-and-swap guard. PublicationKey, owner,
// authority, mandate, approval and Run fields are deliberately not accepted.
type DoerDraftInput struct {
	CreationKey     string                `json:"creation_key,omitempty"`
	ID              string                `json:"id,omitempty"`
	ExpectedVersion uint64                `json:"expected_version"`
	Agent           reference.RevisionRef `json:"agent"`
	LoopID          string                `json:"loop_id"`
	Revision        uint64                `json:"revision"`
	PreviousDigest  string                `json:"previous_digest,omitempty"`
	Contract        loop.DoerContract     `json:"contract"`
}

func (input *DoerDraftInput) UnmarshalJSON(data []byte) error {
	if len(data) > DoerDraftMaxBytes {
		return ErrDoerDraftInvalid
	}
	if err := reference.RejectDuplicateObjectKeys(data); err != nil {
		return err
	}
	type wire DoerDraftInput
	var value wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return ErrDoerDraftInvalid
	}
	*input = DoerDraftInput(value)
	return nil
}

// DoerDraft never constitutes an approval or execution intent. Expiry is fixed
// at creation and is not extended by edits. Exact Agent identity is retained
// even if later Registry changes make publication inadmissible.
type DoerDraft struct {
	CreationKey    string                `json:"creation_key,omitempty"`
	CreationDigest string                `json:"creation_digest,omitempty"`
	ID             string                `json:"id"`
	PrincipalID    string                `json:"principal_id"`
	PublicationKey string                `json:"publication_key"`
	Version        uint64                `json:"version"`
	Agent          reference.RevisionRef `json:"agent"`
	LoopID         string                `json:"loop_id"`
	Revision       uint64                `json:"revision"`
	PreviousDigest string                `json:"previous_digest,omitempty"`
	Contract       loop.DoerContract     `json:"contract"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
	ExpiresAt      time.Time             `json:"expires_at"`
}

// One bounded principal record lets Store.Update's interprocess lock serialize
// both capacity admission and optimistic edits. Expired records remain retained:
// there is no hard-delete, implicit recycling or fresh publication intent.
type doerDraftSet struct {
	PrincipalID string      `json:"principal_id"`
	Drafts      []DoerDraft `json:"drafts"`
}

func doerDraftSlot(principal string) string {
	sum := sha256.Sum256([]byte(principal))
	return hex.EncodeToString(sum[:])
}

func validateDoerDraftInput(input DoerDraftInput) error {
	if len(input.CreationKey) > 128 || strings.TrimSpace(input.CreationKey) != input.CreationKey || (input.ID != "" && input.CreationKey != "") {
		return ErrDoerDraftInvalid
	}
	if (input.ID == "" && input.ExpectedVersion != 0) || (input.ID != "" && (!doerDraftIDPattern.MatchString(input.ID) || input.ExpectedVersion == 0)) || input.Agent.Validate() != nil {
		return ErrDoerDraftInvalid
	}
	wire, err := json.Marshal(input)
	if err != nil || len(wire) > DoerDraftMaxBytes {
		return ErrDoerDraftInvalid
	}
	if _, _, err := loop.NewDoerRevision(input.LoopID, input.Revision, input.PreviousDigest, input.Contract); err != nil {
		return ErrDoerDraftInvalid
	}
	return nil
}

func decodeDoerDraftSet(raw []byte, principal string) (doerDraftSet, error) {
	var set doerDraftSet
	if len(raw) > DoerDraftMaxRetained*DoerDraftMaxBytes*2 {
		return set, ErrDoerDraftInvalid
	}
	if err := reference.RejectDuplicateObjectKeys(raw); err != nil {
		return set, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&set); err != nil {
		return set, err
	}
	if set.PrincipalID != principal {
		return set, ErrDenied
	}
	if len(set.Drafts) > DoerDraftMaxRetained {
		return set, ErrDoerDraftInvalid
	}
	seen, keys, creationKeys := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, draft := range set.Drafts {
		if draft.PrincipalID != principal {
			return set, ErrDenied
		}
		if !doerDraftIDPattern.MatchString(draft.ID) || !doerDraftKeyPattern.MatchString(draft.PublicationKey) || seen[draft.ID] || keys[draft.PublicationKey] || draft.Version == 0 || draft.CreatedAt.IsZero() || draft.UpdatedAt.Before(draft.CreatedAt) || !draft.UpdatedAt.Before(draft.ExpiresAt) || !draft.ExpiresAt.Equal(draft.CreatedAt.Add(DoerDraftTTL)) {
			return set, ErrDoerDraftInvalid
		}
		if draft.CreationKey != "" {
			if len(draft.CreationKey) > 128 || strings.TrimSpace(draft.CreationKey) != draft.CreationKey || creationKeys[draft.CreationKey] || !strings.HasPrefix(draft.CreationDigest, "sha256:") || len(draft.CreationDigest) != 71 {
				return set, ErrDoerDraftInvalid
			}
			if _, err := hex.DecodeString(strings.TrimPrefix(draft.CreationDigest, "sha256:")); err != nil {
				return set, ErrDoerDraftInvalid
			}
			creationKeys[draft.CreationKey] = true
		} else if draft.CreationDigest != "" {
			return set, ErrDoerDraftInvalid
		}
		if err := validateDoerDraftInput(DoerDraftInput{ID: draft.ID, ExpectedVersion: draft.Version, Agent: draft.Agent, LoopID: draft.LoopID, Revision: draft.Revision, PreviousDigest: draft.PreviousDigest, Contract: draft.Contract}); err != nil {
			return set, err
		}
		seen[draft.ID], keys[draft.PublicationKey] = true, true
	}
	return set, nil
}

func (s *Service) SaveDoerDraft(ctx context.Context, input DoerDraftInput) (DoerDraft, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return DoerDraft{}, err
	}
	return s.SaveDoerDraftAs(ctx, subject, input)
}

// SaveDoerDraftAs accepts only a subject authenticated by a trusted transport,
// as with the other As APIs. Saving does not resolve or replace the Agent,
// publish a Loop, grant authority, provision a runtime or create a Queue item.
func (s *Service) SaveDoerDraftAs(ctx context.Context, subject core.Subject, input DoerDraftInput) (DoerDraft, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return DoerDraft{}, err
	}
	if err := ctx.Err(); err != nil {
		return DoerDraft{}, err
	}
	if s.Store == nil {
		return DoerDraft{}, ErrFleetUnavailable
	}
	if err := validateDoerDraftInput(input); err != nil {
		return DoerDraft{}, err
	}
	// Detach caller-owned pointers/slices before persistence and returned readback.
	wire, _ := json.Marshal(input)
	if err := json.Unmarshal(wire, &input); err != nil {
		return DoerDraft{}, err
	}
	slot := doerDraftSlot(subject.PrincipalID)
	if input.ID == "" {
		err := s.Store.Create(doerDraftKind, slot, doerDraftSet{PrincipalID: subject.PrincipalID, Drafts: []DoerDraft{}})
		if err != nil && !errors.Is(err, store.ErrAlreadyExists) {
			return DoerDraft{}, err
		}
	}
	var result DoerDraft
	err := s.Store.Update(doerDraftKind, slot, func(raw []byte) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.requirePrincipal(subject); err != nil {
			return nil, err
		}
		set, err := decodeDoerDraftSet(raw, subject.PrincipalID)
		if err != nil {
			return nil, err
		}
		now := s.Now().UTC()
		index := -1
		if input.ID == "" {
			var creationDigest string
			if input.CreationKey != "" {
				sum := sha256.Sum256(wire)
				creationDigest = "sha256:" + hex.EncodeToString(sum[:])
				for _, existing := range set.Drafts {
					if existing.CreationKey == input.CreationKey {
						if existing.CreationDigest != creationDigest {
							return nil, ErrConflict
						}
						if !now.Before(existing.ExpiresAt) {
							return nil, ErrExpired
						}
						result = existing
						return set, nil
					}
				}
			}
			if len(set.Drafts) >= DoerDraftMaxRetained {
				return nil, ErrDoerDraftCapacity
			}
			result = DoerDraft{ID: store.ID("doerdraft"), PrincipalID: subject.PrincipalID, PublicationKey: store.ID("doerpub"), CreatedAt: now, ExpiresAt: now.Add(DoerDraftTTL), CreationKey: input.CreationKey, CreationDigest: creationDigest}
		} else {
			for i, draft := range set.Drafts {
				if draft.ID == input.ID {
					index, result = i, draft
					break
				}
			}
			if index < 0 {
				return nil, ErrDenied
			}
			if !now.Before(result.ExpiresAt) {
				return nil, ErrExpired
			}
			if result.Version != input.ExpectedVersion || result.Version == ^uint64(0) || now.Before(result.UpdatedAt) {
				return nil, ErrConflict
			}
			// Approval workflows cannot silently retarget a saved intent to a new Agent.
			if input.Agent != result.Agent {
				return nil, ErrConflict
			}
			// Returning from an independent approval to an unchanged draft must
			// retain its reviewed version and keys, not invalidate continuation.
			if input.LoopID == result.LoopID && input.Revision == result.Revision && input.PreviousDigest == result.PreviousDigest && reflect.DeepEqual(input.Contract, result.Contract) {
				return set, nil
			}
		}
		result.Version++
		result.UpdatedAt = now
		result.Agent, result.LoopID, result.Revision, result.PreviousDigest, result.Contract = input.Agent, input.LoopID, input.Revision, input.PreviousDigest, input.Contract
		if index < 0 {
			set.Drafts = append(set.Drafts, result)
		} else {
			set.Drafts[index] = result
		}
		return set, nil
	})
	if err != nil {
		return DoerDraft{}, err
	}
	return result, nil
}

func (s *Service) ReadDoerDraft(ctx context.Context, id string) (DoerDraft, error) {
	subject, err := s.Authenticate(ctx)
	if err != nil {
		return DoerDraft{}, err
	}
	return s.ReadDoerDraftAs(ctx, subject, id)
}

func (s *Service) ReadDoerDraftAs(ctx context.Context, subject core.Subject, id string) (DoerDraft, error) {
	if err := s.RequirePrincipalIdentity(subject); err != nil {
		return DoerDraft{}, err
	}
	if err := ctx.Err(); err != nil {
		return DoerDraft{}, err
	}
	if !doerDraftIDPattern.MatchString(id) {
		return DoerDraft{}, ErrDoerDraftInvalid
	}
	if s.Store == nil {
		return DoerDraft{}, ErrFleetUnavailable
	}
	var raw json.RawMessage
	if err := s.Store.Load(doerDraftKind, doerDraftSlot(subject.PrincipalID), &raw); err != nil {
		return DoerDraft{}, err
	}
	set, err := decodeDoerDraftSet(raw, subject.PrincipalID)
	if err != nil {
		return DoerDraft{}, err
	}
	for _, draft := range set.Drafts {
		if draft.ID == id {
			if !s.Now().Before(draft.ExpiresAt) {
				return DoerDraft{}, ErrExpired
			}
			return draft, nil
		}
	}
	return DoerDraft{}, ErrDenied
}

// ValidateDoerDraftPublicationAs is a non-authorizing publication preflight.
// The eventual publication service must still independently admit its exact
// publisher and authority; this check creates neither approval nor execution.
func (s *Service) ValidateDoerDraftPublicationAs(ctx context.Context, subject core.Subject, id string, version uint64) (DoerDraft, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return DoerDraft{}, err
	}
	draft, err := s.ReadDoerDraftAs(ctx, subject, id)
	if err != nil {
		return DoerDraft{}, err
	}
	if draft.Version != version {
		return DoerDraft{}, ErrConflict
	}
	if s.FleetRepository == nil {
		return DoerDraft{}, ErrFleetUnavailable
	}
	exact, err := s.FleetRepository.GetAgentRevision(ctx, draft.Agent.ID, draft.Agent.Revision)
	if err != nil {
		return DoerDraft{}, err
	}
	latest, err := s.FleetRepository.LatestAgentRevision(ctx, draft.Agent.ID)
	if err != nil {
		return DoerDraft{}, err
	}
	if agentRevisionRef(exact) != draft.Agent || agentRevisionRef(latest) != draft.Agent || exact.Lifecycle != registry.LifecycleEnabled || latest.Lifecycle != registry.LifecycleEnabled || exact.Ownership.OwnerID != subject.PrincipalID || latest.Ownership.OwnerID != subject.PrincipalID {
		return DoerDraft{}, ErrDenied
	}
	return draft, nil
}
