package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/store"
)

const loopModelEditKind = "loop-model-edits"

var loopModelEditID = regexp.MustCompile(`^loopedit-[0-9a-f]{64}$`)

// LoopModelEdit is an inert, immutable proposal. It is not publication, consent,
// a provider connection, a mandate, or an execution intent. The original complete
// candidate survives independent charter approval without reconstruction.
type LoopModelEdit struct {
	// Read-only projections; never persisted as proposal authority.
	Status          string                `json:"status,omitempty"`
	Publication     *PublishedLoop        `json:"publication,omitempty"`
	ID              string                `json:"id"`
	PrincipalID     string                `json:"principal_id"`
	Input           PublishLoopInput      `json:"input"`
	Expected        reference.RevisionRef `json:"expected_agent"`
	Original        core.CanonicalCharter `json:"original_charter"`
	Proposed        core.CanonicalCharter `json:"proposed_charter"`
	MissingModel    core.HermesLane       `json:"missing_model"`
	CreatedAt       time.Time             `json:"created_at"`
	ExpiresAt       time.Time             `json:"expires_at"`
	ConfirmationURL string                `json:"confirmation_url"`
}

type loopModelEditSet struct {
	Edits []LoopModelEdit `json:"edits"`
}

func loopEditID(principal, key string) string {
	raw, _ := json.Marshal([]string{principal, key})
	sum := sha256.Sum256(raw)
	return "loopedit-" + hex.EncodeToString(sum[:])
}

// SubmitOwnedLoopEditAs keeps normal publication for an approved model. Only a
// registered workspace owner's immediate successor can enter the pending path.
// Publication itself continues to use PublishLoopAs, with all its normal checks.
func (s *Service) SubmitOwnedLoopEditAs(ctx context.Context, sub core.Subject, input PublishLoopInput) (PublishedLoop, error) {
	if err := s.requireFleetDashboardPrincipal(sub); err != nil {
		return PublishedLoop{}, err
	}
	if input.AgentID == "" || input.Authority != (reference.DigestRef{}) || input.Publisher != (reference.RevisionRef{}) || strings.TrimSpace(input.IdempotencyKey) != input.IdempotencyKey || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return PublishedLoop{}, ErrConflict
	}
	candidate, _, err := loop.NewRevision(input.Revision)
	if err != nil {
		return PublishedLoop{}, err
	}
	input.Revision = candidate
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > DoerDraftMaxBytes {
		return PublishedLoop{}, ErrConflict
	}
	// Detach the exact proposed bytes from callers before retention.
	if err = json.Unmarshal(raw, &input); err != nil {
		return PublishedLoop{}, err
	}
	// Resolve retained outcomes before asking for today's mutation authority.
	// A historical publication is readback, not a new publication/approval.
	held, err := s.ReadOwnedLoopEditAs(ctx, sub, loopEditID(sub.PrincipalID, input.IdempotencyKey))
	if err == nil {
		if !reflect.DeepEqual(held.Input, input) {
			return PublishedLoop{}, ErrConflict
		}
		return loopModelEditResult(held)
	}
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, fleet.ErrNotFound) {
		return PublishedLoop{}, err
	}
	published, err := s.ownedLoopEditPublication(ctx, sub, input)
	if err != nil {
		return PublishedLoop{}, err
	}
	if published != nil {
		return *published, nil
	}
	workspace, err := s.RegisteredAgentWorkspaceAs(ctx, sub, input.AgentID)
	if err != nil {
		return PublishedLoop{}, err
	}
	if err = s.ownedLoopEditPredecessor(ctx, sub, input); err != nil {
		return PublishedLoop{}, err
	}
	if s.validateLoopCharterLanes(ctx, workspace.Agent, candidate, "", "") == nil {
		return s.PublishLoopAs(ctx, sub, input)
	}
	agent, err := s.GetFleetAgentAs(ctx, sub, workspace.Agent.ID, workspace.Agent.Revision)
	if err != nil {
		return PublishedLoop{}, err
	}
	original, err := s.GetCharter(agent.Revision.Charter.ID, agent.Revision.Charter.Revision)
	if err != nil || original.Digest != agent.Revision.Charter.Digest {
		return PublishedLoop{}, ErrConflict
	}
	revision := original.Charter.Revision
	charters, err := s.ListCharters(original.Charter.AgentID)
	if err != nil {
		return PublishedLoop{}, err
	}
	for _, c := range charters {
		if c.Charter.Revision > revision {
			revision = c.Charter.Revision
		}
	}
	if revision == ^uint64(0) {
		return PublishedLoop{}, ErrConflict
	}
	proposed, missing, err := loopEditCharter(original, candidate, revision+1)
	if err != nil {
		return PublishedLoop{}, err
	}
	// Reuse an exact imported proposal when present. Import alone never approves.
	for _, c := range charters {
		if c.Charter.Revision <= original.Charter.Revision || c.Charter.Revision >= proposed.Charter.Revision {
			continue
		}
		want := proposed.Charter
		want.Revision, want.CreatedAt, want.CreatedBy = c.Charter.Revision, c.Charter.CreatedAt, c.Charter.CreatedBy
		exact, e := core.Canonicalize(want)
		if e == nil && exact.Digest == c.Digest {
			proposed = exact
		}
	}
	now := s.Now().UTC()
	edit := LoopModelEdit{ID: loopEditID(sub.PrincipalID, input.IdempotencyKey), PrincipalID: sub.PrincipalID, Input: input, Expected: workspace.Agent, Original: original, Proposed: proposed, MissingModel: missing, CreatedAt: now, ExpiresAt: now.Add(DoerDraftTTL)}
	edit.ConfirmationURL = "/console/loops/model-edit?edit_id=" + url.QueryEscape(edit.ID)
	encoded, err := json.Marshal(edit)
	if err != nil || len(encoded) > DoerDraftMaxBytes*4 {
		return PublishedLoop{}, ErrConflict
	}
	if err = s.archiveLoopModelEdits(ctx, sub); err != nil {
		return PublishedLoop{}, err
	}
	slot := doerDraftSlot(sub.PrincipalID)
	if err = s.Store.Create(loopModelEditKind, slot, loopModelEditSet{Edits: []LoopModelEdit{}}); err != nil && !errors.Is(err, store.ErrAlreadyExists) {
		return PublishedLoop{}, err
	}
	err = s.Store.Update(loopModelEditKind, slot, func(raw []byte) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fresh, err := s.RegisteredAgentWorkspaceAs(ctx, sub, input.AgentID)
		if err != nil {
			return nil, err
		}
		if fresh.Agent != edit.Expected {
			return nil, ErrConflict
		}
		set, err := decodeLoopModelEdits(raw, sub.PrincipalID)
		if err != nil {
			return nil, err
		}
		for _, old := range set.Edits {
			if old.ID == edit.ID {
				if !reflect.DeepEqual(old.Input, input) {
					return nil, ErrConflict
				}
				edit = old
				return set, nil
			}
		}
		archived, historyErr := s.loadLoopModelEditHistory(sub.PrincipalID, edit.ID)
		if historyErr == nil {
			if !reflect.DeepEqual(archived.Input, input) {
				return nil, ErrConflict
			}
			edit = archived
			return set, nil
		}
		if !errors.Is(historyErr, os.ErrNotExist) {
			return nil, historyErr
		}
		set, err = s.pendingLoopModelEdits(ctx, sub, set)
		if err != nil {
			return nil, err
		}
		if len(set.Edits) >= DoerDraftMaxRetained {
			return nil, ErrDoerDraftCapacity
		}
		set.Edits = append(set.Edits, edit)
		return set, nil
	})
	if err != nil {
		return PublishedLoop{}, err
	}
	held, err = s.ReadOwnedLoopEditAs(ctx, sub, edit.ID)
	if err != nil {
		return PublishedLoop{}, err
	}
	return loopModelEditResult(held)
}

func (s *Service) ownedLoopEditPredecessor(ctx context.Context, sub core.Subject, input PublishLoopInput) error {
	if input.Revision.Revision < 2 || input.ExpectedPreviousDigest != input.Revision.PreviousDigest {
		return ErrConflict
	}
	owner, err := s.FleetRepository.GetLoopPublicationProvenance(ctx, input.Revision.LoopID, 1)
	if err != nil {
		return err
	}
	if owner.AuthorityKind != "registered-agent-workspace" || owner.OwnerID != sub.PrincipalID || owner.PublisherAgent.ID != input.AgentID {
		return ErrDenied
	}
	previous, err := s.FleetRepository.GetLoopRevision(ctx, input.Revision.LoopID, input.Revision.Revision-1)
	if err != nil {
		return err
	}
	if previous.Digest != input.ExpectedPreviousDigest {
		return ErrConflict
	}
	occupied, err := s.FleetRepository.GetLoopRevision(ctx, input.Revision.LoopID, input.Revision.Revision)
	if err == nil && occupied.Digest != input.Revision.Digest {
		return ErrConflict
	}
	if err != nil && !errors.Is(err, fleet.ErrNotFound) {
		return err
	}
	return nil
}

// Narrow automatic design: one unambiguous enabled stanza and one exact missing
// model. No union, default change, tool/credential removal or runtime fallback.
func loopEditCharter(original core.CanonicalCharter, candidate loop.LoopRevision, revision uint64) (core.CanonicalCharter, core.HermesLane, error) {
	fail := func() (core.CanonicalCharter, core.HermesLane, error) {
		return core.CanonicalCharter{}, core.HermesLane{}, ErrDenied
	}
	check, err := core.Canonicalize(original.Charter)
	if err != nil || check.Digest != original.Digest || candidate.Contribution != nil {
		return fail()
	}
	var c core.Charter
	if json.Unmarshal(check.Canonical, &c) != nil {
		return fail()
	}
	selected := -1
	for i, st := range c.Stanzas {
		if st.Enabled {
			if selected != -1 {
				return fail()
			}
			selected = i
		}
	}
	if selected < 0 {
		return fail()
	}
	h := &c.Stanzas[selected].Hermes
	missing := core.HermesLane{}
	for _, step := range candidate.Steps {
		p, m := step.EffectiveLane()
		if p == "" && m == "" {
			continue
		}
		if _, err := h.ResolveLane(p, m); err == nil {
			continue
		}
		// Model-only unresolved requests cannot select a provider by inference.
		if p == "" || m == "" {
			return fail()
		}
		if missing.Provider != "" && (missing.Provider != p || missing.Model != m) {
			return fail()
		}
		missing.Provider, missing.Model = p, m
	}
	if missing.Provider == "" {
		return fail()
	}
	return addExactModelLane(c, selected, missing, revision)
}

// Shared narrow design for an operator request or a retained Loop edit.
func addExactModelLane(c core.Charter, selected int, missing core.HermesLane, revision uint64) (core.CanonicalCharter, core.HermesLane, error) {
	fail := func() (core.CanonicalCharter, core.HermesLane, error) {
		return core.CanonicalCharter{}, core.HermesLane{}, ErrDenied
	}
	h := &c.Stanzas[selected].Hermes
	found := false
	for _, lane := range h.EffectiveLanes() {
		if lane.Provider != missing.Provider {
			continue
		}
		// A local model's artifact digest cannot be borrowed for another model.
		if lane.LocalInference != nil {
			return fail()
		}
		if found && !reflect.DeepEqual(missing.ProviderAuthentication, lane.ProviderAuthentication) {
			return fail()
		}
		missing.ProviderAuthentication = lane.ProviderAuthentication
		found = true
	}
	if !found {
		switch missing.Provider {
		case "codex", "openai-codex":
			missing.ProviderAuthentication = &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}
		default:
			return fail()
		}
	}
	if len(h.AllowedLanes) == 0 {
		h.AllowedLanes = h.EffectiveLanes()
	}
	h.AllowedLanes = append(h.AllowedLanes, missing)
	c.Revision = revision
	proposed, err := core.Canonicalize(c)
	return proposed, missing, err
}

func decodeLoopModelEdits(raw []byte, principal string) (loopModelEditSet, error) {
	var set loopModelEditSet
	if len(raw) > DoerDraftMaxRetained*DoerDraftMaxBytes*4 || reference.RejectDuplicateObjectKeys(raw) != nil {
		return set, ErrConflict
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&set) != nil || len(set.Edits) > DoerDraftMaxRetained {
		return set, ErrConflict
	}
	seen := map[string]bool{}
	for _, e := range set.Edits {
		if e.Status != "" || e.Publication != nil || e.PrincipalID != principal || e.ID != loopEditID(principal, e.Input.IdempotencyKey) || seen[e.ID] || e.Expected.Validate() != nil || e.Expected.ID != e.Input.AgentID || e.CreatedAt.IsZero() || !e.ExpiresAt.Equal(e.CreatedAt.Add(DoerDraftTTL)) || e.ConfirmationURL != "/console/loops/model-edit?edit_id="+url.QueryEscape(e.ID) {
			return set, ErrConflict
		}
		candidate, _, err := loop.NewRevision(e.Input.Revision)
		if err != nil || candidate.Digest != e.Input.Revision.Digest {
			return set, ErrConflict
		}
		proposed, missing, err := loopEditCharter(e.Original, candidate, e.Proposed.Charter.Revision)
		if err != nil {
			return set, ErrConflict
		}
		proposed.Charter.CreatedAt, proposed.Charter.CreatedBy = e.Proposed.Charter.CreatedAt, e.Proposed.Charter.CreatedBy
		canonical, err := core.Canonicalize(proposed.Charter)
		supplied, e2 := core.Canonicalize(e.Proposed.Charter)
		if err != nil || e2 != nil || supplied.Digest != e.Proposed.Digest || canonical.Digest != supplied.Digest || !reflect.DeepEqual(missing, e.MissingModel) {
			return set, ErrConflict
		}
		seen[e.ID] = true
	}
	return set, nil
}

func (s *Service) ReadOwnedLoopEditAs(ctx context.Context, sub core.Subject, id string) (LoopModelEdit, error) {
	if err := s.requireFleetPrincipalIdentity(sub); err != nil {
		return LoopModelEdit{}, err
	}
	if s.Store == nil {
		return LoopModelEdit{}, ErrFleetUnavailable
	}
	if err := ctx.Err(); err != nil {
		return LoopModelEdit{}, err
	}
	if !loopModelEditID.MatchString(id) {
		return LoopModelEdit{}, ErrConflict
	}
	var raw json.RawMessage
	if err := s.Store.Load(loopModelEditKind, doerDraftSlot(sub.PrincipalID), &raw); err != nil {
		return LoopModelEdit{}, err
	}
	set, err := decodeLoopModelEdits(raw, sub.PrincipalID)
	if err != nil {
		return LoopModelEdit{}, err
	}
	for _, e := range set.Edits {
		if e.ID == id {
			return s.projectLoopModelEdit(ctx, sub, e)
		}
	}
	e, err := s.loadLoopModelEditHistory(sub.PrincipalID, id)
	if err != nil {
		return LoopModelEdit{}, err
	}
	return s.projectLoopModelEdit(ctx, sub, e)
}

// ConfirmOwnedLoopModelAs is reachable only from the Console's single-use exact
// review receipt. Peer/local-OS callers cannot approve through this seam. The
// trusted adapter supplies a real live password subject; no identity is upgraded.
func (s *Service) ConfirmOwnedLoopModelAs(ctx context.Context, sub core.Subject, review LoopModelEdit) (FleetAgent, error) {
	s.agentRevisionMu.Lock()
	defer s.agentRevisionMu.Unlock()
	admit := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if sub.Kind != "principal" || sub.Issuer != "aegis-principal-auth" || sub.Method != "password" || s.DashboardActionAuthentication == nil || s.DashboardActionAuthentication(sub) != nil || s.requireFleetPrincipal(sub) != nil || !s.Now().Before(review.ExpiresAt) {
			return ErrDenied
		}
		return nil
	}
	if err := admit(); err != nil {
		return FleetAgent{}, err
	}
	held, err := s.ReadOwnedLoopEditAs(ctx, sub, review.ID)
	if err != nil {
		return FleetAgent{}, err
	}
	// CanonicalCharter contains RawMessage: review HTML and durable storage may
	// indent it differently. Compare complete compact JSON, not whitespace in
	// those raw fields; every value and both canonical digests remain pinned.
	heldWire, heldErr := json.Marshal(held)
	reviewWire, reviewErr := json.Marshal(review)
	if held.Status != "pending" || heldErr != nil || reviewErr != nil || !bytes.Equal(heldWire, reviewWire) {
		return FleetAgent{}, ErrConflict
	}
	if err = s.ownedLoopEditPredecessor(ctx, sub, held.Input); err != nil {
		return FleetAgent{}, err
	}
	old, err := s.GetFleetAgentAs(ctx, sub, held.Expected.ID, held.Expected.Revision)
	if err != nil {
		return FleetAgent{}, err
	}
	if agentRevisionRef(old.Revision) != held.Expected || old.Revision.Charter.Digest != held.Original.Digest {
		return FleetAgent{}, ErrConflict
	}
	current, err := s.GetFleetAgentAs(ctx, sub, held.Expected.ID, 0)
	if err != nil {
		return FleetAgent{}, err
	}
	target := RevisionReference(held.Expected.ID, held.Proposed.Charter.Revision, held.Proposed.Digest)
	if agentRevisionRef(current.Revision) != held.Expected {
		if current.Revision.Charter == target && s.validAgentCharterSuccessor(old.Revision, current.Revision) {
			return current, nil
		}
		return FleetAgent{}, ErrConflict
	}
	// The stored proposal is not authority. Only this fresh independent decision
	// imports and approves its exact canonical charter, with repeated admission.
	canonical, err := core.Canonicalize(held.Proposed.Charter)
	if err != nil || canonical.Digest != held.Proposed.Digest {
		return FleetAgent{}, ErrConflict
	}
	existing, err := s.GetCharter(target.ID, target.Revision)
	if err == nil {
		if existing.Digest != target.Digest {
			return FleetAgent{}, ErrConflict
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err = s.Store.SaveCharterAdmitted(canonical, admit); err != nil {
			return FleetAgent{}, err
		}
	} else {
		return FleetAgent{}, err
	}
	return s.approveAgentCharterLocked(ctx, sub, target.ID, ApproveAgentCharterInput{Expected: held.Expected, Charter: target}, admit)
}

// PublishOwnedLoopEditAs resumes the exact retained bytes. Independent setup
// grants no publication or Run; the owner invokes this separately. A missing
// permission still returns the original normal denial, never an approval.
func (s *Service) PublishOwnedLoopEditAs(ctx context.Context, sub core.Subject, id string) (PublishedLoop, error) {
	if err := s.requireFleetDashboardPrincipal(sub); err != nil {
		return PublishedLoop{}, err
	}
	held, err := s.ReadOwnedLoopEditAs(ctx, sub, id)
	if err != nil {
		return PublishedLoop{}, err
	}
	result, err := loopModelEditResult(held)
	if err != nil || held.Status == "published" {
		return result, err
	}
	return s.PublishLoopAs(ctx, sub, held.Input)
}
