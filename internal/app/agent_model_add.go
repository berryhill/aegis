package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// AgentModelAddInput is an exact operator selector, never a charter supplied by a model.
type AgentModelAddInput struct {
	AgentID  string `json:"agent_id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}
type AgentModelAddReview struct {
	Input     AgentModelAddInput    `json:"input"`
	Expected  reference.RevisionRef `json:"expected"`
	Original  core.CanonicalCharter `json:"original"`
	Proposed  core.CanonicalCharter `json:"proposed"`
	ExpiresAt time.Time             `json:"expires_at"`
	Receipt   string                `json:"receipt,omitempty"`
}

func (s *Service) requireModelAddPrincipal(ctx context.Context, sub core.Subject) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if sub.Kind != "principal" || sub.Issuer != "aegis-principal-auth" || sub.Method != "password" || s.DashboardActionAuthentication == nil || s.DashboardActionAuthentication(sub) != nil {
		return ErrDenied
	}
	return s.requireFleetPrincipal(sub)
}

// PrepareAgentModelAddAs requires independent enrolled authentication even for
// review. No draft, import, approval, connection or runtime effect occurs here.
func (s *Service) PrepareAgentModelAddAs(ctx context.Context, sub core.Subject, in AgentModelAddInput) (AgentModelAddReview, error) {
	if err := s.requireModelAddPrincipal(ctx, sub); err != nil {
		return AgentModelAddReview{}, err
	}
	if in.AgentID == "" || in.AgentID == registry.BuiltInAegisAgentID || in.Provider == "" || in.Model == "" || strings.TrimSpace(in.Provider) != in.Provider || strings.TrimSpace(in.Model) != in.Model {
		return AgentModelAddReview{}, ErrConflict
	}
	agent, err := s.GetFleetAgentAs(ctx, sub, in.AgentID, 0)
	if err != nil {
		return AgentModelAddReview{}, err
	}
	if agent.Revision.Ownership.OwnerID != sub.PrincipalID || agent.Revision.Lifecycle != registry.LifecycleEnabled {
		return AgentModelAddReview{}, ErrDenied
	}
	original, err := s.GetCharter(agent.Revision.Charter.ID, agent.Revision.Charter.Revision)
	if err != nil || original.Digest != agent.Revision.Charter.Digest {
		return AgentModelAddReview{}, ErrConflict
	}
	canonical, err := core.Canonicalize(original.Charter)
	if err != nil || canonical.Digest != original.Digest {
		return AgentModelAddReview{}, ErrConflict
	}
	var c core.Charter
	if json.Unmarshal(canonical.Canonical, &c) != nil {
		return AgentModelAddReview{}, ErrConflict
	}
	selected := -1
	for i, st := range c.Stanzas {
		if st.Enabled {
			if selected != -1 {
				return AgentModelAddReview{}, ErrDenied
			}
			selected = i
		}
	}
	if selected < 0 {
		return AgentModelAddReview{}, ErrDenied
	}
	if _, err := c.Stanzas[selected].Hermes.ResolveLane(in.Provider, in.Model); err == nil {
		return AgentModelAddReview{}, ErrConflict
	}
	charters, err := s.ListCharters(c.AgentID)
	if err != nil {
		return AgentModelAddReview{}, err
	}
	revision := c.Revision
	for _, old := range charters {
		if old.Charter.Revision > revision {
			revision = old.Charter.Revision
		}
	}
	if revision == ^uint64(0) {
		return AgentModelAddReview{}, ErrConflict
	}
	proposed, _, err := addExactModelLane(c, selected, core.HermesLane{Provider: in.Provider, Model: in.Model}, revision+1)
	if err != nil {
		return AgentModelAddReview{}, err
	}
	// Reuse a previously imported identical successor, but never its approval.
	for _, old := range charters {
		if old.Charter.Revision <= c.Revision || old.Charter.Revision >= proposed.Charter.Revision {
			continue
		}
		want := proposed.Charter
		want.Revision, want.CreatedAt, want.CreatedBy = old.Charter.Revision, old.Charter.CreatedAt, old.Charter.CreatedBy
		exact, e := core.Canonicalize(want)
		if e == nil && exact.Digest == old.Digest {
			proposed = exact
		}
	}
	return AgentModelAddReview{Input: in, Expected: agentRevisionRef(agent.Revision), Original: original, Proposed: proposed, ExpiresAt: s.Now().UTC().Add(3 * time.Minute)}, nil
}

// ConfirmAgentModelAddAs is called only after consuming a session-bound native
// review receipt. Recompute the narrow proposal and fence every publication.
func (s *Service) ConfirmAgentModelAddAs(ctx context.Context, sub core.Subject, review AgentModelAddReview) (FleetAgent, error) {
	s.agentRevisionMu.Lock()
	defer s.agentRevisionMu.Unlock()
	admit := func() error {
		if err := s.requireModelAddPrincipal(ctx, sub); err != nil {
			return err
		}
		if !s.Now().Before(review.ExpiresAt) {
			return ErrDenied
		}
		return nil
	}
	if err := admit(); err != nil {
		return FleetAgent{}, err
	}
	fresh, err := s.PrepareAgentModelAddAs(ctx, sub, review.Input)
	if err != nil {
		return FleetAgent{}, err
	}
	if fresh.Expected != review.Expected || fresh.Original.Digest != review.Original.Digest || fresh.Proposed.Digest != review.Proposed.Digest {
		return FleetAgent{}, ErrConflict
	}
	// Only recomputed canonical bytes are ever written; client review fields are not authority.
	canonical := fresh.Proposed
	existing, err := s.GetCharter(canonical.Charter.AgentID, canonical.Charter.Revision)
	if err == nil {
		if existing.Digest != canonical.Digest {
			return FleetAgent{}, ErrConflict
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err = s.Store.SaveCharterAdmitted(canonical, admit); err != nil {
			return FleetAgent{}, err
		}
	} else {
		return FleetAgent{}, err
	}
	return s.approveAgentCharterLocked(ctx, sub, review.Input.AgentID, ApproveAgentCharterInput{Expected: review.Expected, Charter: RevisionReference(canonical.Charter.AgentID, canonical.Charter.Revision, canonical.Digest)}, admit)
}
