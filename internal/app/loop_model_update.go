package app

import (
	"context"
	"errors"
	"strings"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
)

var ErrLoopModelEditInput = errors.New("model edit requires distinct declared Hermes step selectors")

// OwnedLoopModelUpdateInput selects a model, not authority. Empty Steps means
// all explicitly declared Hermes turns; deterministic and other backends stay
// byte-for-byte unchanged. The prior revision is an explicit path argument.
type OwnedLoopModelUpdateInput struct {
	AgentID  string   `json:"agent_id"`
	Provider string   `json:"provider"`
	Model    string   `json:"model"`
	Steps    []string `json:"steps,omitempty"`
}

func (s *Service) UpdateOwnedLoopModelAs(ctx context.Context, sub core.Subject, id string, revision uint64, in OwnedLoopModelUpdateInput) (PublishedLoop, error) {
	if err := s.requireFleetDashboardPrincipal(sub); err != nil {
		return PublishedLoop{}, err
	}
	if revision == 0 || revision == ^uint64(0) || in.Provider == "" || in.Model == "" || strings.TrimSpace(in.Provider) != in.Provider || strings.TrimSpace(in.Model) != in.Model {
		return PublishedLoop{}, ErrConflict
	}
	old, err := s.GetLoopAs(ctx, sub, id, revision)
	if err != nil {
		return PublishedLoop{}, err
	}
	selected := map[string]bool{}
	for _, step := range in.Steps {
		if step == "" || selected[step] {
			return PublishedLoop{}, ErrLoopModelEditInput
		}
		selected[step] = true
	}
	// NewRevision deep-copies all candidate structure before modifying its lanes.
	next, _, err := loop.NewRevision(old)
	if err != nil {
		return PublishedLoop{}, err
	}
	count := 0
	for i := range next.Steps {
		step := &next.Steps[i]
		if len(in.Steps) > 0 && !selected[step.ID] {
			continue
		}
		if step.Executable == nil || step.Executable.Backend != "hermes" {
			if len(in.Steps) > 0 {
				return PublishedLoop{}, ErrLoopModelEditInput
			}
			continue
		}
		step.Provider, step.Model = in.Provider, in.Model
		step.Executable.Provider, step.Executable.Model = in.Provider, in.Model
		delete(selected, step.ID)
		count++
	}
	if count == 0 || len(selected) != 0 {
		return PublishedLoop{}, ErrLoopModelEditInput
	}
	next.Revision, next.PreviousDigest, next.Digest = revision+1, old.Digest, ""
	next, _, err = loop.NewRevision(next)
	if err != nil {
		return PublishedLoop{}, err
	}
	return s.SubmitOwnedLoopEditAs(ctx, sub, PublishLoopInput{AgentID: in.AgentID, Revision: next, ExpectedPreviousDigest: old.Digest, IdempotencyKey: "model-edit-" + strings.TrimPrefix(next.Digest, "sha256:")})
}
