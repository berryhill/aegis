package update

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
)

type journeyFixture struct {
	events      []string
	fail        string
	healthy     bool
	image       string
	onPreflight func()
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
	if j.onPreflight != nil {
		j.onPreflight()
	}
	return j.event("preflight")
}

type observedJourneyUpdater struct {
	*Updater
	prepared *Prepared
}

func (u *observedJourneyUpdater) Prepare(ctx context.Context) (*Prepared, error) {
	p, err := u.Updater.Prepare(ctx)
	u.prepared = p
	return p, err
}

func TestJourneyRejectsSubstitutionBeforeStoppingGateway(t *testing.T) {
	for _, which := range []string{"target", "candidate"} {
		t.Run(which, func(t *testing.T) {
			u, _ := sliceCandidate(t, "")
			observed := &observedJourneyUpdater{Updater: u}
			j := &journeyFixture{onPreflight: func() {
				path := observed.prepared.target
				if which == "candidate" {
					path = observed.prepared.candidate
				}
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.Write([]byte("changed during protected authentication"))
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("append=%v close=%v", err, closeErr)
				}
			}}
			r, err := RunJourney(context.Background(), observed, j)
			if err == nil || r.UpgradeComplete {
				t.Fatalf("result=%+v err=%v", r, err)
			}
			for _, event := range j.events {
				if event == "stop" {
					t.Fatalf("changed %s stopped the gateway: %v", which, j.events)
				}
			}
		})
	}
}

func TestPreparedIdentityRejectsChangedSealedBytes(t *testing.T) {
	u, _ := sliceCandidate(t, "")
	p, err := u.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	f, err := os.OpenFile(p.candidate, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write([]byte("uncommitted candidate overlay"))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append=%v close=%v", err, closeErr)
	}
	if identity, err := p.Identity(); err == nil {
		t.Fatalf("unsealed identity admitted: %+v", identity)
	}
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
func (j *journeyFixture) VerifiedConsoleURL() string { return "http://fixture/console" }
func TestProductionJourneyReplacementRecoveryActivationVerification(t *testing.T) {
	for _, failure := range []string{"", "stop", "recover", "activate", "verify", "record:complete"} {
		t.Run(failure, func(t *testing.T) {
			u, _ := sliceCandidate(t, "")
			j := &journeyFixture{fail: failure}
			result, err := RunJourney(context.Background(), u, j)
			if failure != "" {
				if err == nil || result.UpgradeComplete || !result.ResumeRequired {
					t.Fatalf("result=%+v err=%v", result, err)
				}
				if result.RunningVersion != "" || result.RunningSourceRevision != "" || result.AuditCurrent || result.ConsoleVerified || result.Phase == "complete" {
					t.Fatalf("incomplete journey reported completion evidence: %+v", result)
				}
				return
			}
			if err != nil || !result.UpgradeComplete || result.ResumeRequired {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if result.RunningVersion != "1.2.0" || len(result.RunningSourceRevision) != 40 || result.GatewayImage != "running_current_image" || !result.AuditCurrent || !result.ConsoleVerified || result.ConsoleURL != "http://fixture/console" {
				t.Fatalf("verified completion evidence missing: %+v", result)
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
