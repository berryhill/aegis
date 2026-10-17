package update

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
)

type journeyFixture struct {
	events  []string
	fail    string
	healthy bool
	image   string
}

func (j *journeyFixture) event(name string) error {
	j.events = append(j.events, name)
	if j.fail == name {
		return errors.New("injected " + name)
	}
	return nil
}
func (j *journeyFixture) Preflight(_ context.Context, id CandidateIdentity) error {
	j.image = id.Executable
	return j.event("preflight")
}
func (j *journeyFixture) Healthy(context.Context) bool { return j.healthy }
func (j *journeyFixture) Stop(context.Context) error   { return j.event("stop") }
func (j *journeyFixture) Recover(context.Context) error {
	b, e := os.ReadFile(j.image)
	if e != nil || len(b) < 4 || string(b[:4]) != "\x7fELF" {
		return errors.New("recovery ran before real replacement")
	}
	return j.event("recover")
}
func (j *journeyFixture) Activate(context.Context) error { return j.event("activate") }
func (j *journeyFixture) Verify(context.Context) error   { return j.event("verify") }
func (j *journeyFixture) Record(_ context.Context, stage string) error {
	return j.event("record:" + stage)
}
func TestProductionJourneyReplacementRecoveryActivationVerification(t *testing.T) {
	for _, failure := range []string{"", "stop", "recover", "activate", "verify"} {
		t.Run(failure, func(t *testing.T) {
			u, _ := sliceCandidate(t, "")
			j := &journeyFixture{fail: failure}
			result, err := RunJourney(context.Background(), u, j)
			if failure != "" {
				if err == nil || result.UpgradeComplete || !result.ResumeRequired {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				return
			}
			if err != nil || !result.UpgradeComplete || result.ResumeRequired {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			want := []string{"preflight", "record:gateway_stop", "stop", "record:binary_install", "record:authority_validation", "recover", "record:gateway_activation", "activate", "record:verification", "verify", "record:complete"}
			if !reflect.DeepEqual(j.events, want) {
				t.Fatalf("events=%v", j.events)
			}
			// Re-enter with the now-current actual image: candidate preparation and source
			// proof still run; a healthy current service is neither stopped nor restarted.
			u.CurrentVersion = "1.2.0"
			j = &journeyFixture{healthy: true}
			result, err = RunJourney(context.Background(), u, j)
			if err != nil || !result.UpgradeComplete || !reflect.DeepEqual(j.events, []string{"preflight", "verify", "record:complete"}) {
				t.Fatalf("resume result=%+v events=%v err=%v", result, j.events, err)
			}
		})
	}
}
