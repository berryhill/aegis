package app

import (
	"context"
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/orchestration"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/queue"
)

type contributionFenceRead struct {
	*fleetbadger.Store
	mutate  func(*contribution.Continuity)
	readErr error
}

func (r *contributionFenceRead) GetContributionContinuity(ctx context.Context, id string) (contribution.Continuity, []contribution.ContinuityEvent, error) {
	st, events, err := r.Store.GetContributionContinuity(ctx, id)
	if err == nil && r.mutate != nil {
		r.mutate(&st)
	}
	if r.readErr != nil {
		err = r.readErr
	}
	return st, events, err
}

// Reuses the real independently issued sessions; no synthetic authority grants.
func exerciseContributionRuntimeFences(t *testing.T, s *Service, repo *fleetbadger.Store, sub core.Subject, c contribution.Contract, st contribution.Continuity) {
	t.Helper()
	ctx := context.Background()
	original := s.FleetRepository
	defer func() { s.FleetRepository = original }()
	for _, name := range []string{"controller", "builder", "tester"} {
		role := st.RoleAuthorities[name]
		session, err := s.GetSession(role.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		authority, err := s.authorityContextForSession(ctx, role.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		launch := execution.LaunchContract{OwnerID: c.OwnerID, Mandate: session.Mandate, AuthorityContext: authority}
		index := map[string]int{"controller": 0, "builder": 1, "tester": 2}[name]
		checker := contributionRoleAdmission{s, c, index}
		if got, e := checker.CheckRuntimeAdmission(ctx, launch, s.Now()); e != nil || !got.Allowed {
			t.Fatalf("live %s denied: %+v %v", name, got, e)
		}
		for _, tc := range []struct {
			name   string
			change func(*contribution.Continuity)
		}{
			{"stop-requested", func(x *contribution.Continuity) { x.RoleStops = map[string]contribution.RoleStop{name: {}} }},
			{"cancel", func(x *contribution.Continuity) { x.CancelRequested = true }},
			{"epoch", func(x *contribution.Continuity) { x.RoleEpochs = map[string]uint64{name: role.Epoch + 1} }},
			{"missing", func(x *contribution.Continuity) { x.RoleAuthorities = map[string]contribution.RoleAuthority{} }},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				s.FleetRepository = &contributionFenceRead{Store: repo, mutate: tc.change}
				got, e := checker.CheckRuntimeAdmission(ctx, launch, s.Now())
				if e == nil && got.Allowed {
					t.Fatal("fenced role admitted")
				}
				after, _, e := repo.GetContributionContinuity(ctx, c.QueueItemID)
				if e != nil || after.Digest != st.Digest {
					t.Fatal("denial mutated durable state", e)
				}
			})
		}
		s.FleetRepository = original
	}
	// Only a successful canonical absence read may allow the old lease path.
	if _, _, err := s.currentContributionAdmission(ctx, sub, c, queue.Claim{}); !errors.Is(err, orchestration.ErrNoContributionContinuation) {
		t.Fatal("positive absence not distinguished", err)
	}
	s.FleetRepository = &contributionFenceRead{Store: repo, readErr: errors.New("unknown lookup")}
	if _, _, err := s.currentContributionAdmission(ctx, sub, c, queue.Claim{}); err == nil || errors.Is(err, orchestration.ErrNoContributionContinuation) {
		t.Fatal("unknown lookup became absence", err)
	}
	s.FleetRepository = &contributionFenceRead{Store: repo, mutate: func(x *contribution.Continuity) { x.Continuation = &contribution.Continuation{ClaimID: "mismatched"} }}
	if _, _, err := s.currentContributionAdmission(ctx, sub, c, queue.Claim{}); err == nil || errors.Is(err, orchestration.ErrNoContributionContinuation) {
		t.Fatal("invalid continuation became absence", err)
	}
}
