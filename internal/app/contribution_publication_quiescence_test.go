package app

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
)

type publicationQuiescenceTestPort func(context.Context, ContributionPublicationBinding) error

func (p publicationQuiescenceTestPort) QuiesceContributionPublication(ctx context.Context, b ContributionPublicationBinding) error {
	return p(ctx, b)
}

// Composition-boundary test only, not qualification of #378's remote store.
func exercisePublicationQuiescencePort(t *testing.T, s *Service, c contribution.Contract, state contribution.Continuity) {
	t.Helper()
	c.Effect = "merge"
	ctx := context.Background()
	calls := 0
	portErr := errors.New("unknown remote result")
	s.ContributionPublicationQuiescer = publicationQuiescenceTestPort(func(_ context.Context, b ContributionPublicationBinding) error {
		calls++
		r := state.RoleAuthorities["controller"]
		if b.Contract.ID != c.QueueItemID || b.Contract.Digest != c.Digest || b.RunID != c.RunID || b.QueueItemID != c.QueueItemID || b.Candidate != state.CandidateEvidence || b.Controller != r.Ref() || b.Epoch != r.Epoch {
			t.Fatal("publication binding substituted")
		}
		return portErr
	})
	defer func() { s.ContributionPublicationQuiescer = nil }()
	if s.quiesceContributionPublication(ctx, c, state) || calls != 0 {
		t.Fatal("port called before sticky cancellation")
	}
	state.CancelRequested = true
	port := s.ContributionPublicationQuiescer
	s.ContributionPublicationQuiescer = nil
	if s.quiesceContributionPublication(ctx, c, state) {
		t.Fatal("missing port default allowed")
	}
	s.ContributionPublicationQuiescer = port
	if s.quiesceContributionPublication(ctx, c, state) || calls != 1 {
		t.Fatal("unknown remote falsely quiescent")
	}
	portErr = nil
	if !s.quiesceContributionPublication(ctx, c, state) || calls != 2 {
		t.Fatal("exact owning port not consumed")
	}
	state.RoleEpochs["controller"]++
	if s.quiesceContributionPublication(ctx, c, state) || calls != 2 {
		t.Fatal("epoch substitution reached port")
	}
	state.RoleEpochs["controller"]--
}
