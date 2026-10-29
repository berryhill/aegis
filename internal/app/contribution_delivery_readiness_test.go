package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
)

func TestContributionReadinessGenericForeignDenied(t *testing.T) {
	ctx := context.Background()
	s, _, subject, ref, reader := contributionAuthorityFixture(t)
	c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "readiness", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	for _, counter := range []string{"infrastructure-recheck", "candidate", "queue-attempt", "runtime-turn", "decision-delivery"} {
		if _, err = s.RecordContributionContinuityAs(ctx, subject, "foreign-controller", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: st.Sequence, ExpectedDigest: st.Digest, Kind: "reserve", Counter: counter}); err == nil {
			t.Fatal("foreign generic reserve")
		}
	}
}

// These records are accounting fixtures, NOT native effect admission or grants.
func readinessAccountingRole(t *testing.T, s *Service, sub core.Subject, c contribution.Contract, st contribution.Continuity) contribution.Continuity {
	t.Helper()
	ctx := context.Background()
	binding, _ := contribution.SealedRole(c, "controller")
	m, _, err := s.PreviewSessionAs(ctx, sub, binding.Charter.ID, binding.Charter.Revision, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.StartSessionAs(ctx, sub, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.TerminateSession(ctx, session.ID, "cleanup") })
	authority, err := s.authorityContextForSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.BindQueueRuntimeAs(ctx, sub, BindQueueRuntimeInput{AgentID: c.Controller.ID, Authority: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: authority.ID, Digest: authority.Digest}, QueueItemID: c.QueueItemID, BindingID: "root-binding", TransitionID: "root-bound"})
	if err != nil {
		t.Fatal(err)
	}
	input := func() ContributionContinuityInput {
		return ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: st.Sequence, ExpectedDigest: st.Digest}
	}
	st, err = s.AttachContributionObservationAs(ctx, sub, session.ID, c.Key, input())
	if err != nil {
		t.Fatal(err)
	}
	st, err = s.AdmitContributionRoleAs(ctx, sub, session.ID, c.Key, "controller", input())
	if err != nil {
		t.Fatal(err)
	}
	in := input()
	in.Kind = "checkpoint"
	in.Stage = "checks"
	in.Candidate = "candidate"
	in.Evidence = reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "candidate", Digest: contribution.Digest([]byte("candidate"))}
	st, err = s.RecordContributionContinuityAs(ctx, sub, c.SessionID, c.Key, in)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// This tests the post-admission accounting seam with the real Service and Badger.
// It supplies no private proof and does not claim native admission coverage.
func TestContributionReadinessAttemptAccounting(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(fmt.Sprint(concurrent), func(t *testing.T) {
			ctx := context.Background()
			s, _, subject, ref, reader := contributionAuthorityFixture(t)
			c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "readiness-attempt", "local-only", reader)
			if err != nil {
				t.Fatal(err)
			}
			state, err := s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
			if err != nil {
				t.Fatal(err)
			}
			state = readinessAccountingRole(t, s, subject, c, state)
			i := delivery.Intent{Operation: delivery.CheckDelivery, Reference: delivery.Reference{Contract: c.Digest, Run: c.RunID}}
			if concurrent {
				var wg sync.WaitGroup
				errs := make(chan error, 2)
				for n := 0; n < 2; n++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						errs <- s.appendContributionReadinessReservation(ctx, state.RoleAuthorities["controller"].SessionID, i, 2, c, state)
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
				if err = s.appendContributionReadinessReservation(ctx, state.RoleAuthorities["controller"].SessionID, i, 2, c, state); err != nil {
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
			if err = s.appendContributionReadinessReservation(ctx, state.RoleAuthorities["controller"].SessionID, i, 2, c, old); err == nil {
				t.Fatal("stale CAS granted HTTP")
			}
			// Same exact UNKNOWN intent, fresh controller: charge NEXT slot, not a veto.
			if err = s.appendContributionReadinessReservation(ctx, state.RoleAuthorities["controller"].SessionID, i, 2, c, state); err != nil {
				t.Fatal(err)
			}
			_, state, _, err = s.ObserveContributionContinuityAs(ctx, subject, c.Key)
			if err != nil || state.Used.InfrastructureRechecks != 2 || len(state.Reservations) != 2 {
				t.Fatalf("two attempts: %+v %v", state, err)
			}
			for _, op := range []delivery.Operation{delivery.CheckDelivery, delivery.Merge} {
				i.Operation = op
				if err = s.appendContributionReadinessReservation(ctx, state.RoleAuthorities["controller"].SessionID, i, 2, c, state); !errors.Is(err, delivery.ErrReadinessExhausted) {
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
	s, store, subject, ref, reader := contributionAuthorityFixture(t)
	c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "readiness-loss", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	state = readinessAccountingRole(t, s, subject, c, state)
	i := delivery.Intent{Operation: delivery.CheckDelivery, Reference: delivery.Reference{Contract: c.Digest, Run: c.RunID}}
	// Actual Badger commits, then only the response is replaced with an error.
	s.FleetRepository = readinessLostResponseStore{store}
	if err = s.appendContributionReadinessReservation(ctx, state.RoleAuthorities["controller"].SessionID, i, 2, c, state); err == nil {
		t.Fatal("lost response granted read")
	}
	s.FleetRepository = store
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if err != nil || state.Used.InfrastructureRechecks != 1 {
		t.Fatalf("lost charge %+v %v", state, err)
	}
	if err = s.appendContributionReadinessReservation(ctx, state.RoleAuthorities["controller"].SessionID, i, 2, c, state); err != nil {
		t.Fatal(err)
	}
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if err != nil || state.Used.InfrastructureRechecks != 2 {
		t.Fatalf("next charge %+v %v", state, err)
	}
	if err = s.appendContributionReadinessReservation(ctx, state.RoleAuthorities["controller"].SessionID, i, 2, c, state); !errors.Is(err, delivery.ErrReadinessExhausted) {
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
