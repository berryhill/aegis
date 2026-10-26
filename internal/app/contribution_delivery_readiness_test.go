package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
)

func TestContributionReadinessCanonicalReservations(t *testing.T) {
	ctx := context.Background()
	s, _, subject, ref, reader := contributionFixture(t)
	c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "readiness", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	input := func(id string) ContributionContinuityInput {
		return ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "reserve", Counter: "infrastructure-recheck", OperationID: id}
	}
	first := input("readiness/check/1")
	// Generic Record remains original-session-only; role ports must not weaken it.
	if _, err := s.RecordContributionContinuityAs(ctx, subject, "foreign-controller-session", c.Key, first); err == nil {
		t.Fatal("generic foreign-session reservation admitted")
	}
	// Simultaneous attempts use one exact head and one canonical operation ID.
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, first)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else if !errors.Is(e, contribution.ErrContinuityConflict) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatalf("CAS winners %d", success)
	}
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if err != nil || state.Used.InfrastructureRechecks != 1 || state.Reservations[first.OperationID] != "infrastructure-recheck" {
		t.Fatalf("unknown-response readback %+v %v", state, err)
	}
	// Fresh service calls and a changed checkpoint do not replenish root spending.
	checkpoint := ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "checkpoint", Stage: "merge", Candidate: "candidate-two", Evidence: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "candidate-two", Digest: contribution.Digest([]byte("second"))}}
	state, err = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	// Replaying the same operation at a fresh head must not double consume.
	state, err = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, input(first.OperationID))
	if err != nil || state.Used.InfrastructureRechecks != 1 {
		t.Fatalf("replay %+v %v", state, err)
	}
	state, err = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, input("readiness/merge/2"))
	if err != nil || state.Used.InfrastructureRechecks != 2 {
		t.Fatalf("second %+v %v", state, err)
	}
	_, err = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, input("readiness/check/3"))
	if !errors.Is(err, contribution.ErrContinuityExhausted) {
		t.Fatal(fmt.Sprintf("global ceiling: %v", err))
	}
	foreign := subject
	foreign.PrincipalID = "foreign"
	if _, err = s.RecordContributionContinuityAs(ctx, foreign, "original", c.Key, input("foreign")); err == nil {
		t.Fatal("foreign spend")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.RecordContributionContinuityAs(cancelled, subject, "original", c.Key, input("cancelled")); err == nil {
		t.Fatal("cancelled spend")
	}
}

// This tests the post-admission accounting seam with the real Service and Badger.
// It supplies no private proof and does not claim native admission coverage.
func TestContributionReadinessAttemptAccounting(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrent), func(t *testing.T) {
			ctx := context.Background()
			s, _, subject, ref, reader := contributionFixture(t)
			c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "readiness-attempt", "local-only", reader)
			if err != nil {
				t.Fatal(err)
			}
			state, err := s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
			if err != nil {
				t.Fatal(err)
			}
			i := delivery.Intent{Operation: delivery.CheckDelivery, Reference: delivery.Reference{Contract: c.Digest, Run: c.RunID}}
			if concurrent {
				var wg sync.WaitGroup
				errs := make(chan error, 2)
				for n := 0; n < 2; n++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						errs <- s.appendContributionReadinessReservation(ctx, "original", i, 2, c, state)
					}()
				}
				wg.Wait()
				close(errs)
				winners := 0
				for e := range errs {
					if e == nil {
						winners++
					}
				}
				if winners != 1 {
					t.Fatalf("permissions for same global slot: %d", winners)
				}
			} else {
				if err = s.appendContributionReadinessReservation(ctx, "original", i, 2, c, state); err != nil {
					t.Fatal(err)
				}
			}
			// No HTTP occurs: simulates crash before HTTP / response loss with charge held.
			old := state
			_, state, _, err = s.ObserveContributionContinuityAs(ctx, subject, c.Key)
			if err != nil || state.Used.InfrastructureRechecks != 1 {
				t.Fatalf("held charge: %+v %v", state, err)
			}
			// An ambiguous duplicate cannot acquire another caller's accepted permission.
			if err = s.appendContributionReadinessReservation(ctx, "original", i, 2, c, old); err == nil {
				t.Fatal("stale CAS granted HTTP")
			}
			// Same exact UNKNOWN intent, fresh controller: charge NEXT slot, not a veto.
			if err = s.appendContributionReadinessReservation(ctx, "original", i, 2, c, state); err != nil {
				t.Fatal(err)
			}
			_, state, _, err = s.ObserveContributionContinuityAs(ctx, subject, c.Key)
			if err != nil || state.Used.InfrastructureRechecks != 2 || len(state.Reservations) != 2 {
				t.Fatalf("two attempts: %+v %v", state, err)
			}
			for _, op := range []delivery.Operation{delivery.CheckDelivery, delivery.Merge} {
				i.Operation = op
				if err = s.appendContributionReadinessReservation(ctx, "original", i, 2, c, state); !errors.Is(err, delivery.ErrReadinessExhausted) {
					t.Fatalf("global ceiling %v", err)
				}
			}
		})
	}
}

type readinessLostResponseStore struct{ *fleetbadger.Store }

func (s readinessLostResponseStore) AppendContributionContinuity(ctx context.Context, e contribution.ContinuityEvent) (contribution.Continuity, error) {
	state, err := s.Store.AppendContributionContinuity(ctx, e)
	if err == nil {
		return state, errors.New("test transport response lost after actual commit")
	}
	return state, err
}

func TestContributionReadinessLostResponseRetainsCharge(t *testing.T) {
	ctx := context.Background()
	s, store, subject, ref, reader := contributionFixture(t)
	c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "readiness-loss", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	i := delivery.Intent{Operation: delivery.CheckDelivery, Reference: delivery.Reference{Contract: c.Digest, Run: c.RunID}}
	// Actual Badger commits, then only the response is replaced with an error.
	s.FleetRepository = readinessLostResponseStore{store}
	if err = s.appendContributionReadinessReservation(ctx, "original", i, 2, c, state); err == nil {
		t.Fatal("lost response granted read")
	}
	s.FleetRepository = store
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if err != nil || state.Used.InfrastructureRechecks != 1 {
		t.Fatalf("lost charge %+v %v", state, err)
	}
	if err = s.appendContributionReadinessReservation(ctx, "original", i, 2, c, state); err != nil {
		t.Fatal(err)
	}
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if err != nil || state.Used.InfrastructureRechecks != 2 {
		t.Fatalf("next charge %+v %v", state, err)
	}
	if err = s.appendContributionReadinessReservation(ctx, "original", i, 2, c, state); !errors.Is(err, delivery.ErrReadinessExhausted) {
		t.Fatalf("exhaustion: %v", err)
	}
}

func TestContributionReadinessRequiresHeldPrivateContext(t *testing.T) {
	ctx := context.Background()
	budget := &contributionDeliveryReadiness{live: ctx, productive: &productiveContributionAuthority{}, instructions: &contributionInstructionAuthority{}}
	for _, maximum := range []uint8{0, 1, 2, 3} {
		if err := budget.ReserveReadiness(ctx, delivery.Intent{Operation: delivery.CheckDelivery}, maximum); err == nil {
			t.Fatal("inactive private source accepted")
		}
	}
	if err := budget.ReserveReadiness(context.WithValue(ctx, struct{}{}, true), delivery.Intent{Operation: delivery.Merge}, 1); err == nil {
		t.Fatal("replacement context accepted")
	}
}
