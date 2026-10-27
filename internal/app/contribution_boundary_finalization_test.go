package app

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/disposition"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
)

type retainedResultReadFailure struct {
	*fleetbadger.Store
	failure error
	reads   int
}

func (r *retainedResultReadFailure) GetDispositionByGraphRun(context.Context, string) (disposition.Record, error) {
	r.reads++
	return disposition.Record{}, r.failure
}

func TestRetainedContributionFinalizationPreservesResultReadFailure(t *testing.T) {
	s, _, subject, loop, reader := contributionFixture(t)
	ctx := context.Background()
	c, err := s.admitContributionAs(ctx, subject, "session", loop, reader.source.URL.URL, "retained-result-error", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	// A canonical read failure is not absence and must terminate before any
	// verification, policy resolution, custody entry or terminal write.
	failure := errors.New("controlled indexed result read failure")
	repository := &retainedResultReadFailure{Store: s.FleetRepository.(*fleetbadger.Store), failure: failure}
	s.FleetRepository = repository
	v := &ContributionFinalVerifier{service: s}
	if _, err = v.FinalizeRetainedContributionOutcomeAs(ctx, subject, c.Key); !errors.Is(err, failure) {
		t.Fatalf("indexed read failure discarded: %v", err)
	}
	if repository.reads != 1 {
		t.Fatalf("read replayed: %d", repository.reads)
	}
}
