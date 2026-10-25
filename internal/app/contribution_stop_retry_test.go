package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"testing"
)

type contributionStopRaceRepository struct {
	*fleetbadger.Store
	cancel context.CancelFunc
	race   bool
}

func (r *contributionStopRaceRepository) AppendContributionContinuity(ctx context.Context, e contribution.ContinuityEvent) (contribution.Continuity, error) {
	if e.Kind == "role-stop-acknowledged" && r.race {
		r.race = false
		other := e
		other.Kind = "observation"
		other.RoleStop = nil
		other, _ = contribution.SealContinuityEvent(other)
		if _, err := r.Store.AppendContributionContinuity(ctx, other); err != nil {
			return contribution.Continuity{}, err
		}
	}
	state, err := r.Store.AppendContributionContinuity(ctx, e)
	if err == nil && e.Kind == "role-stop-requested" && r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return state, err
}
func exerciseContributionStopRetry(t *testing.T, s *Service, repo *fleetbadger.Store, sub core.Subject, c contribution.Contract, state contribution.Continuity) contribution.Continuity {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wrapped := &contributionStopRaceRepository{Store: repo, cancel: cancel, race: true}
	s.FleetRepository = wrapped
	defer func() { s.FleetRepository = repo }()
	input := func() ContributionContinuityInput {
		return ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}
	}
	var err error
	state, err = s.StopContributionRoleAs(ctx, sub, c.Key, "tester", input())
	if err != nil || !state.RoleStops["tester"].Acknowledged || ctx.Err() != context.Canceled {
		t.Fatalf("lost caller and ack race: %+v %v", state, err)
	}
	retained, events, err := repo.GetContributionContinuity(context.Background(), c.QueueItemID)
	if err != nil || retained.Digest != state.Digest {
		t.Fatalf("durable completion: %v", err)
	}
	requests, acks := 0, 0
	for _, e := range events {
		if e.RoleStop == nil || e.RoleStop.Authority.Role != "tester" {
			continue
		}
		if e.Kind == "role-stop-requested" {
			requests++
		}
		if e.Kind == "role-stop-acknowledged" {
			acks++
		}
	}
	if requests != 1 || acks != 1 {
		t.Fatalf("duplicate stop facts: %d %d", requests, acks)
	}
	// Retained pending request from a lost response BEFORE any signal is safe to retry.
	builder := state.RoleAuthorities["builder"]
	sess, err := s.GetSession(builder.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	stop := contribution.RoleStop{Authority: builder, Epoch: builder.Epoch, PID: sess.RuntimePID, ProcessStart: sess.ProcessStart}
	event, err := contribution.SealContinuityEvent(contribution.ContinuityEvent{SchemaVersion: contribution.ContinuitySchema, QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: builder.SessionID, Sequence: state.Sequence + 1, PreviousDigest: state.Digest, Kind: "role-stop-requested", RoleStop: &stop, At: s.Now()})
	if err != nil {
		t.Fatal(err)
	}
	state, err = repo.AppendContributionContinuity(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.StopContributionRoleAs(context.Background(), sub, c.Key, "builder", input())
	if err != nil || !state.RoleStops["builder"].Acknowledged {
		t.Fatalf("pending retry: %v", err)
	}
	before := state.Digest
	state, err = s.StopContributionRoleAs(context.Background(), sub, c.Key, "tester", input())
	if err != nil || state.Digest != before {
		t.Fatalf("confirmed stop replay: %+v %v", state, err)
	}
	return state
}
