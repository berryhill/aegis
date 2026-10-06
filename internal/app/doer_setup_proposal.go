package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// DoerSetupProposal contains no credentials or authority. Adapters must bind it
// to a separate authenticated, exact decision; constructing it is read-only.
type DoerSetupProposal struct {
	DraftID      string                `json:"draft_id"`
	DraftVersion uint64                `json:"draft_version"`
	Expected     reference.RevisionRef `json:"expected"`
	Original     core.CanonicalCharter `json:"original"`
	Proposed     core.CanonicalCharter `json:"proposed"`
}

func (s *Service) ProposeDoerSetupSuccessorAs(ctx context.Context, sub core.Subject, id string, version uint64) (DoerSetupProposal, error) {
	if err := s.requirePrincipal(sub); err != nil {
		return DoerSetupProposal{}, err
	}
	d, err := s.ReadDoerDraftAs(ctx, sub, id)
	if err != nil {
		return DoerSetupProposal{}, err
	}
	if d.Version != version {
		return DoerSetupProposal{}, ErrConflict
	}
	agent, err := s.GetFleetAgentAs(ctx, sub, d.Agent.ID, d.Agent.Revision)
	if err != nil {
		return DoerSetupProposal{}, err
	}
	latest, err := s.GetFleetAgentAs(ctx, sub, d.Agent.ID, 0)
	if err != nil {
		return DoerSetupProposal{}, err
	}
	if agentRevisionRef(latest.Revision) != d.Agent {
		// A failed post-approval draft CAS may leave the exact immediate Agent
		// successor durable. Re-review only that correction, never new authority.
		if !s.validAgentCharterSuccessor(agent.Revision, latest.Revision) || latest.Revision.Ownership.OwnerID != sub.PrincipalID || latest.Revision.Lifecycle != registry.LifecycleEnabled {
			return DoerSetupProposal{}, ErrConflict
		}
		proposal, e := s.doerSetupProposal(ctx, sub, d, latest.Revision.Charter.Revision)
		if e != nil {
			return DoerSetupProposal{}, e
		}
		persisted, e := s.GetCharter(latest.Revision.Charter.ID, latest.Revision.Charter.Revision)
		if e != nil {
			return DoerSetupProposal{}, e
		}
		want := proposal.Proposed.Charter
		want.CreatedBy, want.CreatedAt = persisted.Charter.CreatedBy, persisted.Charter.CreatedAt
		canonical, e := core.Canonicalize(want)
		if e != nil || canonical.Digest != persisted.Digest || canonical.Digest != latest.Revision.Charter.Digest {
			return DoerSetupProposal{}, ErrConflict
		}
		proposal.Proposed = canonical
		return proposal, nil
	}
	revision := agent.Revision.Charter.Revision
	charters, err := s.ListCharters(d.Agent.ID)
	if err != nil {
		return DoerSetupProposal{}, err
	}
	for _, ch := range charters {
		if ch.Charter.Revision > revision {
			revision = ch.Charter.Revision
		}
	}
	if revision == ^uint64(0) {
		return DoerSetupProposal{}, ErrConflict
	}
	proposal, err := s.doerSetupProposal(ctx, sub, d, revision+1)
	if err != nil {
		return DoerSetupProposal{}, err
	}
	// Reuse the earliest exact imported credential-only correction. Import is
	// not approval: the adapter must still obtain an independent exact decision.
	for _, ch := range charters {
		if ch.Charter.Revision <= agent.Revision.Charter.Revision {
			continue
		}
		candidate, e := core.Canonicalize(ch.Charter)
		if e != nil || candidate.Digest != ch.Digest {
			continue
		}
		want := proposal.Proposed.Charter
		want.Revision, want.CreatedBy, want.CreatedAt = ch.Charter.Revision, ch.Charter.CreatedBy, ch.Charter.CreatedAt
		canonical, e := core.Canonicalize(want)
		if e == nil && canonical.Digest == candidate.Digest && candidate.Charter.Revision < proposal.Proposed.Charter.Revision {
			proposal.Proposed = candidate
		}
	}
	return proposal, nil
}

func (s *Service) doerSetupProposal(ctx context.Context, sub core.Subject, d DoerDraft, revision uint64) (DoerSetupProposal, error) {
	agent, err := s.GetFleetAgentAs(ctx, sub, d.Agent.ID, d.Agent.Revision)
	if err != nil {
		return DoerSetupProposal{}, err
	}
	if agentRevisionRef(agent.Revision) != d.Agent || agent.Revision.Ownership.OwnerID != sub.PrincipalID || agent.Revision.Lifecycle != registry.LifecycleEnabled || revision <= agent.Revision.Charter.Revision {
		return DoerSetupProposal{}, ErrConflict
	}
	original, err := s.GetCharter(agent.Revision.Charter.ID, agent.Revision.Charter.Revision)
	if err != nil || original.Digest != agent.Revision.Charter.Digest {
		return DoerSetupProposal{}, ErrConflict
	}
	canonical, err := core.Canonicalize(original.Charter)
	if err != nil || canonical.Digest != original.Digest {
		return DoerSetupProposal{}, ErrConflict
	}
	// Design is not session selection. Browser authentication may not satisfy
	// the runtime's local-OS policy, and must never be relabeled to do so.
	// Only a sole enabled stanza is eligible for this bounded proposal; all
	// runtime selection remains a fresh independent admission after approval.
	var selected *core.TrustStanza
	for i := range original.Charter.Stanzas {
		if !original.Charter.Stanzas[i].Enabled {
			continue
		}
		if selected != nil {
			return DoerSetupProposal{}, errors.New("doer_setup_unique_stanza_required")
		}
		selected = &original.Charter.Stanzas[i]
	}
	if selected == nil {
		return DoerSetupProposal{}, errors.New("doer_setup_unique_stanza_required")
	}
	// Automatic design is deliberately narrower than arbitrary charter editing.
	// Do not erase tools, capabilities, memory or persistent integrations merely
	// to make the runtime eligible. Only legacy provider:codex is corrected.
	if len(selected.Grant.Capabilities) != 0 || len(selected.Grant.Tools) != 0 || len(selected.Scopes.Memory) != 0 || len(selected.Scopes.Credentials) != 1 || selected.Scopes.Credentials[0] != "provider:codex" || selected.Hermes.Profile != "" || selected.Hermes.PersistentHome || len(selected.Hermes.Toolsets) != 0 || len(selected.Hermes.MCPServers) != 0 || len(selected.Hermes.Plugins) != 0 {
		return DoerSetupProposal{}, errors.New("doer_setup_credential_only_correction_required")
	}
	h := selected.Hermes
	if (h.Provider != "codex" && h.Provider != "openai-codex") || h.Model == "" || h.Model == "none" || h.LocalInference != nil {
		return DoerSetupProposal{}, errors.New("doer_setup_controller_codex_required")
	}
	// Deep copy: never modify an existing canonical charter or other stanza.
	raw, err := json.Marshal(original.Charter)
	if err != nil {
		return DoerSetupProposal{}, err
	}
	var c core.Charter
	if err = json.Unmarshal(raw, &c); err != nil {
		return DoerSetupProposal{}, err
	}
	c.Revision, c.CreatedBy, c.CreatedAt = revision, sub.PrincipalID, d.UpdatedAt
	for i := range c.Stanzas {
		stanza := &c.Stanzas[i]
		if stanza.ID != selected.ID {
			continue
		}
		stanza.Scopes.Credentials = nil
		stanza.Hermes.ProviderAuthentication = &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}
	}
	proposed, err := core.Canonicalize(c)
	if err != nil {
		return DoerSetupProposal{}, err
	}
	return DoerSetupProposal{DraftID: d.ID, DraftVersion: d.Version, Expected: d.Agent, Original: canonical, Proposed: proposed}, nil
}

// ConfirmDoerSetupSuccessorAs must be invoked only after the adapter consumes an
// authenticated exact review receipt. It issues no mandate, provisioning,
// host-write approval, publication or Run. Partial import/successor writes are
// retained, not silently retried with a different proposal or widened authority.
func (s *Service) ConfirmDoerSetupSuccessorAs(ctx context.Context, sub core.Subject, proposal DoerSetupProposal) (DoerDraft, error) {
	if err := s.requirePrincipal(sub); err != nil {
		return DoerDraft{}, err
	}
	d, err := s.ReadDoerDraftAs(ctx, sub, proposal.DraftID)
	if err != nil {
		return DoerDraft{}, err
	}
	if d.Version != proposal.DraftVersion || d.Agent != proposal.Expected {
		return DoerDraft{}, ErrConflict
	}
	fresh, err := s.doerSetupProposal(ctx, sub, d, proposal.Proposed.Charter.Revision)
	if err != nil {
		return DoerDraft{}, err
	}
	if existing, e := s.GetCharter(d.Agent.ID, proposal.Proposed.Charter.Revision); e == nil {
		// Imported metadata is retained only for this exact persisted candidate.
		want := fresh.Proposed.Charter
		want.CreatedBy, want.CreatedAt = existing.Charter.CreatedBy, existing.Charter.CreatedAt
		canonical, e := core.Canonicalize(want)
		if e != nil || canonical.Digest != existing.Digest {
			return DoerDraft{}, ErrConflict
		}
		fresh.Proposed = canonical
	} else if !errors.Is(e, os.ErrNotExist) {
		return DoerDraft{}, e
	}
	if fresh.Original.Digest != proposal.Original.Digest || fresh.Proposed.Digest != proposal.Proposed.Digest {
		return DoerDraft{}, ErrConflict
	}
	checked, err := core.Canonicalize(proposal.Proposed.Charter)
	if err != nil || checked.Digest != fresh.Proposed.Digest {
		return DoerDraft{}, ErrConflict
	}
	ref := RevisionReference(d.Agent.ID, checked.Charter.Revision, checked.Digest)
	latest, err := s.GetFleetAgentAs(ctx, sub, d.Agent.ID, 0)
	if err != nil {
		return DoerDraft{}, err
	}
	if agentRevisionRef(latest.Revision) != d.Agent {
		// Existing typed service alone validates identical immediate-successor
		// recovery. Never import another charter after Registry authority changed.
		if latest.Revision.Charter != ref {
			return DoerDraft{}, ErrConflict
		}
		return s.ApproveDoerDraftSuccessorAs(ctx, sub, d.ID, d.Version, ApproveAgentCharterInput{Expected: d.Agent, Charter: ref})
	}
	// Exact import readback is idempotent; a conflicting occupied revision denies.
	existing, err := s.GetCharter(ref.ID, ref.Revision)
	if err == nil {
		if existing.Digest != ref.Digest {
			return DoerDraft{}, ErrConflict
		}
	} else if errors.Is(err, os.ErrNotExist) {
		imported, err := s.ImportCharterAs(ctx, sub, checked.Canonical)
		if err != nil {
			return DoerDraft{}, err
		}
		if imported.Digest != ref.Digest {
			return DoerDraft{}, ErrConflict
		}
	} else {
		return DoerDraft{}, err
	}
	return s.ApproveDoerDraftSuccessorAs(ctx, sub, d.ID, d.Version, ApproveAgentCharterInput{Expected: d.Agent, Charter: ref})
}
