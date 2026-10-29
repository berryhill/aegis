package orchestration

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/execution"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestContributionControllerRevocationFencesLiveChildEffects(t *testing.T) {
	service, _, _, _, _, _ := fleetServiceFixture(t)
	auth := service.authority.(fleetAuthorityRepository)
	now := service.now()
	launch := execution.LaunchContract{OwnerID: auth.mandate.Subject.PrincipalID, Mandate: auth.mandate, AuthorityContext: auth.authority, ParentDispatch: execution.Dispatch{ID: "controller-dispatch", AuthorityContextID: auth.authority.ID, State: execution.StateSucceeded, RequestedAt: now, StartedAt: &now, FinishedAt: &now}}
	live := contributionAdmissionFunc(func(_ context.Context, l execution.LaunchContract, at time.Time) (execution.AdmissionDecision, error) {
		return execution.AdmissionDecision{Allowed: true, AuthorityContextID: l.AuthorityContext.ID, AuthorityContextDigest: l.AuthorityContext.Digest, CheckedAt: at}, nil
	})
	child := &ContributionRole{Launch: launch, Admission: live}
	if err := admitContributionEffects(context.Background(), launch, live, child, now); err != nil {
		t.Fatal("live fixture", err)
	}
	revoked := errors.New("controller revoked")
	denied := contributionAdmissionFunc(func(context.Context, execution.LaunchContract, time.Time) (execution.AdmissionDecision, error) {
		return execution.AdmissionDecision{}, revoked
	})
	for _, effect := range []string{"source-write", "native-command", "runtime-prompt"} {
		t.Run(effect, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "value.go")
			if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			err := admitContributionEffects(context.Background(), launch, denied, child, now)
			if err == nil {
				_ = os.WriteFile(target, []byte("forbidden effect"), 0600)
			}
			if !errors.Is(err, revoked) {
				t.Fatal("live child bypassed controller", err)
			}
			b, e := os.ReadFile(target)
			if e != nil || string(b) != "unchanged" {
				t.Fatal("denied effect mutated source", e)
			}
			if err = admitContributionEffects(context.Background(), launch, live, child, now); err != nil {
				t.Fatal("child no longer live", err)
			}
		})
	}
}
