package managergateway

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
)

// Both independent adapters obtain their opaque preclosure from the real
// verifier in the same source frame. Neither adapter fabricates proof.
type concurrentPreclosure struct {
	primary, duplicate *app.ContributionCompletionAdapter
	t                  *testing.T
}

func (a concurrentPreclosure) VerifyContributionCompletion(ctx context.Context, r delivery.Reference) (string, error) {
	started := time.Now()
	first, err := a.primary.VerifyContributionCompletion(ctx, r)
	a.t.Logf("contribution-stage=primary-preclosure elapsed=%s error=%v", time.Since(started), err)
	if err != nil {
		return first, err
	}
	started = time.Now()
	second, err := a.duplicate.VerifyContributionCompletion(ctx, r)
	a.t.Logf("contribution-stage=duplicate-preclosure elapsed=%s error=%v", time.Since(started), err)
	if err != nil {
		return second, err
	}
	if first != second {
		return "", errors.New("independent preclosure references differ")
	}
	return first, nil
}
func (concurrentPreclosure) FinalizeContributionCompletion(context.Context, delivery.Reference) error {
	return errPauseFinalContribution
}

// Test-only synchronization observes the actual bounded reconciliation read,
// then releases the real canonical writer. Calling B synchronously before
// releasing A would deadlock the test schedule, not test winner reconciliation.
type concurrentCompletionProbe struct {
	*continuationCompletionProbe
	entered, release, observed chan struct{}
	observedOnce               sync.Once
	failWriter                 bool
	t                          *testing.T
	started                    time.Time
}

type concurrentWaiterContextKey struct{}

func (p *concurrentCompletionProbe) CompleteContributionQueueItem(ctx context.Context, c fleet.Completion, f fleet.AuditFact, r fleet.EvidenceReader) error {
	p.t.Logf("contribution-stage=canonical-commit-arrival elapsed=%s", time.Since(p.started))
	close(p.entered)
	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	if p.failWriter {
		return errors.New("controlled canonical writer failure")
	}
	return p.continuationCompletionProbe.CompleteContributionQueueItem(ctx, c, f, r)
}
func (p *concurrentCompletionProbe) GetDispositionByGraphRun(ctx context.Context, run string) (disposition.Record, error) {
	if _, bounded := ctx.Deadline(); bounded && ctx.Value(concurrentWaiterContextKey{}) == p {
		p.observedOnce.Do(func() {
			p.t.Logf("contribution-stage=duplicate-reconciliation-arrival elapsed=%s", time.Since(p.started))
			close(p.observed)
		})
	}
	return p.Store.GetDispositionByGraphRun(ctx, run)
}
func exerciseConcurrentFinalizers(t *testing.T, s *app.Service, sub core.Subject, key string, ref delivery.Reference, primary, duplicate *app.ContributionCompletionAdapter, base *continuationCompletionProbe) (error, bool) {
	t.Helper()
	p := &concurrentCompletionProbe{continuationCompletionProbe: base, entered: make(chan struct{}), release: make(chan struct{}), observed: make(chan struct{}), failWriter: strings.Contains(t.Name(), "WinnerFailure"), t: t, started: time.Now()}
	s.FleetRepository = p
	defer func() { s.FleetRepository = base }()
	before, err := s.ReadContributionExecutionAs(context.Background(), sub, key)
	if err != nil {
		t.Fatal(err)
	}
	winner, loser := make(chan error, 1), make(chan error, 1)
	// Observe the real commit boundary within the existing package alarm, not
	// an unrelated 30-second timer that includes race-instrumented verification.
	deadline, ok := t.Deadline()
	if !ok {
		t.Fatal("bounded test deadline required")
	}
	writerCtx, cancelWriter := context.WithDeadline(context.Background(), deadline.Add(-10*time.Second))
	defer cancelWriter()
	go func() { winner <- primary.FinalizeContributionCompletionAs(writerCtx, sub, ref) }()
	select {
	case <-p.entered:
	case early := <-winner:
		t.Fatalf("writer ended before canonical commit: %v", early)
	case <-writerCtx.Done():
		close(p.release)
		cancelled := <-winner
		t.Fatalf("writer did not reach canonical commit: %v", cancelled)
	}
	loserCtx, cancel := context.WithCancel(context.WithValue(context.Background(), concurrentWaiterContextKey{}, p))
	defer cancel()
	go func() { loser <- duplicate.FinalizeContributionCompletionAs(loserCtx, sub, ref) }()
	select {
	case <-p.observed:
	case <-time.After(4 * time.Second):
		close(p.release)
		<-winner
		<-loser
		t.Fatal("waiter did not reach real reconciliation")
	}
	var loserErr error
	cancelling := strings.Contains(t.Name(), "WaiterCancellation")
	if cancelling {
		cancel()
		loserErr = <-loser
		if !errors.Is(loserErr, context.Canceled) {
			t.Error("waiter cancellation", loserErr)
		}
	}
	close(p.release)
	if !cancelling {
		loserErr = <-loser
	}
	winnerErr := <-winner
	indexed, indexErr := s.GetContributionResultAs(context.Background(), sub, key)
	t.Logf("concurrent canonical writer=%v waiter=%v indexed=%v disposition=%s", winnerErr, loserErr, indexErr, indexed.Disposition.DispositionID)
	after, err := s.ReadContributionExecutionAs(context.Background(), sub, key)
	if err != nil || before.Consumed != after.Consumed || len(before.Passes) != len(after.Passes) {
		t.Fatal("finalizers changed native budget/history", err)
	}
	if p.failWriter {
		if winnerErr == nil || loserErr == nil || !errors.Is(indexErr, fleet.ErrNotFound) {
			t.Fatal("failed writer fabricated completion")
		}
		contract, _, err := s.ReadContributionAs(context.Background(), sub, key)
		if err != nil {
			t.Fatal(err)
		}
		projection, err := p.Store.GetQueueProjection(context.Background(), contract.QueueItemID)
		if err != nil || projection.State != queue.StateClaimed {
			t.Fatal("failed writer changed queue", err)
		}
		return winnerErr, true
	}
	if !cancelling && loserErr != nil {
		t.Error("waiter did not return canonical winner", loserErr)
	}
	if indexErr != nil {
		t.Error("canonical readback", indexErr)
	}
	return winnerErr, false
}
