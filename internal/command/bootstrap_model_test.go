package command

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/onboarding"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/tui"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

func TestOnboardingProgressUsesArtifactDerivedStage(t *testing.T) {
	cases := []struct {
		state     onboarding.State
		completed string
		current   string
	}{
		{onboarding.Uninitialized, "0/5", "local identity and configuration"},
		{onboarding.PrincipalConfigured, "1/5", "credential authority"},
		{onboarding.AuthorityConfigured, "2/5", "Hermes and local Ollama runtime"},
		{onboarding.RuntimeConfigured, "3/5", "exact model binding"},
		{onboarding.ModelPresent, "4/5", "end-to-end certification"},
		{onboarding.Ready, "5/5", "complete"},
	}
	for _, test := range cases {
		t.Run(string(test.state), func(t *testing.T) {
			var output bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&output)
			renderOnboardingProgress(cmd, onboarding.Snapshot{State: test.state})
			if !strings.Contains(output.String(), test.completed) || !strings.Contains(output.String(), test.current) {
				t.Fatalf("progress for %s = %q", test.state, output.String())
			}
		})
	}
}

func TestBareStartupResumesEveryIncompleteManagerOnboardingState(t *testing.T) {
	for _, test := range []struct {
		name        string
		snapshot    onboarding.Snapshot
		authority   authoritybadger.State
		wantBare    bool
		wantManager bool
	}{
		{name: "principal configured", snapshot: onboarding.Snapshot{State: onboarding.PrincipalConfigured}, authority: authoritybadger.StateReady, wantBare: true, wantManager: false},
		{name: "credential authority configured", snapshot: onboarding.Snapshot{State: onboarding.AuthorityConfigured}, authority: authoritybadger.StateReady, wantBare: true, wantManager: false},
		{name: "runtime configured", snapshot: onboarding.Snapshot{State: onboarding.RuntimeConfigured}, authority: authoritybadger.StateReady, wantBare: true, wantManager: false},
		{name: "model present but uncertified", snapshot: onboarding.Snapshot{State: onboarding.ModelPresent}, authority: authoritybadger.StateReady, wantBare: true, wantManager: false},
		{name: "repair required", snapshot: onboarding.Snapshot{State: onboarding.RepairRequired}, authority: authoritybadger.StateReady, wantBare: true, wantManager: false},
		{name: "ready artifacts and authority", snapshot: onboarding.Snapshot{State: onboarding.Ready}, authority: authoritybadger.StateReady, wantBare: false, wantManager: false},
		{name: "ready artifacts but authority unavailable", snapshot: onboarding.Snapshot{State: onboarding.Ready}, authority: authoritybadger.StateAbsent, wantBare: true, wantManager: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := bareRootNeedsBootstrap(test.snapshot, test.authority); got != test.wantBare {
				t.Fatalf("bareRootNeedsBootstrap()=%t want=%t", got, test.wantBare)
			}
			if got := managerNeedsBootstrap(test.snapshot, test.authority); got != test.wantManager {
				t.Fatalf("managerNeedsBootstrap()=%t want=%t", got, test.wantManager)
			}
		})
	}
}

func TestInitCommandOpensFleetForCanonicalBuiltInAgentBootstrap(t *testing.T) {
	root := &cobra.Command{Use: "aegis"}
	initCommand := &cobra.Command{Use: "init"}
	root.AddCommand(initCommand)
	if !commandNeedsFleet(initCommand) {
		t.Fatal("init did not request the fleet store needed for built-in Aegis Agent registration")
	}
}

func TestBootstrapEntryCommandsOpenFleetForCanonicalBuiltInAgent(t *testing.T) {
	root := &cobra.Command{Use: "aegis"}
	manager := &cobra.Command{Use: "manager"}
	root.AddCommand(manager)
	if !commandNeedsFleet(root) {
		t.Fatal("fresh bare root did not request fleet store for built-in Agent bootstrap")
	}
	if !commandNeedsFleet(manager) {
		t.Fatal("aegis manager did not request fleet store for built-in Agent bootstrap")
	}
}

func TestReadyStateBuiltInRegistrationCheckPreservesHealthyGatewayStoreOwnership(t *testing.T) {
	for _, test := range []struct {
		name    string
		state   onboarding.State
		gateway userservice.GatewayState
		want    bool
	}{
		{name: "healthy gateway owns stores", state: onboarding.Ready, gateway: userservice.GatewayHealthy, want: false},
		{name: "ready without gateway", state: onboarding.Ready, gateway: userservice.GatewayNotInstalled, want: true},
		{name: "ready stopped gateway", state: onboarding.Ready, gateway: userservice.GatewayStopped, want: true},
		{name: "incomplete onboarding", state: onboarding.ModelPresent, gateway: userservice.GatewayNotInstalled, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := readyStateNeedsBuiltInRegistrationCheck(onboarding.Snapshot{State: test.state}, test.gateway); got != test.want {
				t.Fatalf("readyStateNeedsBuiltInRegistrationCheck()=%t want=%t", got, test.want)
			}
		})
	}
}

type builtInRegistrationFixture struct {
	existing      app.FleetAgent
	loadErr       error
	registerCalls int
}

func (fixture *builtInRegistrationFixture) GetFleetAgentAs(context.Context, core.Subject, string, uint64) (app.FleetAgent, error) {
	return fixture.existing, fixture.loadErr
}

func (fixture *builtInRegistrationFixture) RegisterBuiltInAegisAgentAs(_ context.Context, subject core.Subject) (app.FleetAgent, bool, error) {
	fixture.registerCalls++
	registration, revision, err := registry.CanonicalBuiltInAegisAgent(subject.PrincipalID)
	return app.FleetAgent{Registration: registration, Revision: revision}, true, err
}

func TestReadyStateBuiltInRegistrationDeclineResumeAndExactReplay(t *testing.T) {
	subject := core.Subject{PrincipalID: "principal"}
	fixture := &builtInRegistrationFixture{loadErr: fleet.ErrNotFound}
	run := func(answer string) (bool, error, string) {
		var output bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		cmd.SetIn(strings.NewReader(answer))
		cmd.SetOut(&output)
		ok, err := bootstrapBuiltInAegisAgentWithService(cmd, fixture, subject, newTerminalInput(cmd.InOrStdin()), newBootstrapPresentation(tui.Capabilities{Width: 100}))
		return ok, err, output.String()
	}

	ok, err, output := run("\n")
	if err != nil || ok || fixture.registerCalls != 0 || !strings.Contains(output, "registration declined; no Agent record was created") {
		t.Fatalf("default decline: ok=%t err=%v calls=%d output=%q", ok, err, fixture.registerCalls, output)
	}
	assertBuiltInAegisRuntimeContract(t, output, "approval")
	ok, err, output = run("yes\n")
	if err != nil || !ok || fixture.registerCalls != 1 || !strings.Contains(output, "registered=true and exactly verified") {
		t.Fatalf("resume: ok=%t err=%v calls=%d output=%q", ok, err, fixture.registerCalls, output)
	}
	assertBuiltInAegisRuntimeContract(t, output, "success")

	registration, revision, err := registry.CanonicalBuiltInAegisAgent(subject.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.existing = app.FleetAgent{Registration: registration, Revision: revision}
	fixture.loadErr = nil
	ok, err, output = run("")
	if err != nil || !ok || fixture.registerCalls != 1 || !strings.Contains(output, "already registered and exactly verified") {
		t.Fatalf("exact replay: ok=%t err=%v calls=%d output=%q", ok, err, fixture.registerCalls, output)
	}
	assertBuiltInAegisRuntimeContract(t, output, "exact replay")

	fixture.existing.Revision.Ownership.OwnerID = "incompatible-owner"
	ok, err, _ = run("yes\n")
	if ok || !errors.Is(err, fleet.ErrConflict) || fixture.registerCalls != 1 {
		t.Fatalf("incompatible registration did not fail closed: ok=%t err=%v calls=%d", ok, err, fixture.registerCalls)
	}
}

func TestBuiltInAegisRegistrationDoesNotCreateHermesProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	subject := core.Subject{PrincipalID: "principal"}
	fixture := &builtInRegistrationFixture{loadErr: fleet.ErrNotFound}
	run := func(answer string) {
		var output bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		cmd.SetIn(strings.NewReader(answer))
		cmd.SetOut(&output)
		ok, err := bootstrapBuiltInAegisAgentWithService(cmd, fixture, subject, newTerminalInput(cmd.InOrStdin()), newBootstrapPresentation(tui.Capabilities{Width: 100}))
		if err != nil || !ok {
			t.Fatalf("registration: ok=%t err=%v output=%q", ok, err, output.String())
		}
	}

	run("yes\n")
	registration, revision, err := registry.CanonicalBuiltInAegisAgent(subject.PrincipalID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.existing = app.FleetAgent{Registration: registration, Revision: revision}
	fixture.loadErr = nil
	run("")

	for _, path := range []string{
		filepath.Join(home, ".hermes"),
		filepath.Join(home, ".hermes", "profiles", registry.BuiltInAegisAgentID),
	} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("logical built-in registration mutated Hermes profile filesystem at %s: %v", path, err)
		}
	}
}

func assertBuiltInAegisRuntimeContract(t *testing.T, output, stage string) {
	t.Helper()
	for _, expected := range []string{
		"agent_id=aegis is a logical Agent Registry identity",
		"runtime_target=manager-disposable is a disposable runtime contract",
		"no ~/.hermes/profiles/aegis is created",
		"supported launch is 'aegis' or 'aegis manager', not 'hermes --profile aegis'",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("%s output missing %q: %q", stage, expected, output)
		}
	}
}

func TestClassifyBareStartupCoversGatewayOwnershipAndOfflineAuthorityStates(t *testing.T) {
	invalid := authoritybadger.Inspection{State: authoritybadger.StateInvalid, Err: errors.New("CLEAN does not authenticate ACTIVE: DIRTY is present")}
	for _, test := range []struct {
		name      string
		snapshot  onboarding.Snapshot
		authority authoritybadger.Inspection
		gateway   userservice.GatewayObservation
		want      bareStartupClass
	}{
		{name: "uninitialized", snapshot: onboarding.Snapshot{State: onboarding.Uninitialized}, authority: authoritybadger.Inspection{State: authoritybadger.StateAbsent}, want: bareStartupUninitialized},
		{name: "resumable bootstrap", snapshot: onboarding.Snapshot{State: onboarding.PrincipalConfigured}, authority: authoritybadger.Inspection{State: authoritybadger.StateAbsent}, want: bareStartupBootstrap},
		{name: "ready without gateway", snapshot: onboarding.Snapshot{State: onboarding.Ready}, authority: authoritybadger.Inspection{State: authoritybadger.StateReady}, gateway: userservice.GatewayObservation{State: userservice.GatewayNotInstalled}, want: bareStartupReadyNoGateway},
		{name: "healthy exact gateway owns open authority", snapshot: onboarding.Snapshot{State: onboarding.Ready}, authority: invalid, gateway: userservice.GatewayObservation{State: userservice.GatewayHealthy}, want: bareStartupGatewayHealthy},
		{name: "installed stopped", snapshot: onboarding.Snapshot{State: onboarding.Ready}, authority: authoritybadger.Inspection{State: authoritybadger.StateReady}, gateway: userservice.GatewayObservation{State: userservice.GatewayStopped}, want: bareStartupGatewayStopped},
		{name: "running unhealthy", snapshot: onboarding.Snapshot{State: onboarding.Ready}, authority: invalid, gateway: userservice.GatewayObservation{State: userservice.GatewayUnhealthy}, want: bareStartupGatewayUnhealthy},
		{name: "mismatched", snapshot: onboarding.Snapshot{State: onboarding.Ready}, authority: invalid, gateway: userservice.GatewayObservation{State: userservice.GatewayMismatched}, want: bareStartupGatewayMismatched},
		{name: "orphaned dirty", snapshot: onboarding.Snapshot{State: onboarding.Ready}, authority: invalid, gateway: userservice.GatewayObservation{State: userservice.GatewayStopped}, want: bareStartupAuthorityOrphaned},
		{name: "corrupt closed", snapshot: onboarding.Snapshot{State: onboarding.Ready}, authority: authoritybadger.Inspection{State: authoritybadger.StateInvalid, Err: errors.New("ACTIVE marker digest invalid")}, gateway: userservice.GatewayObservation{State: userservice.GatewayNotInstalled}, want: bareStartupAuthorityCorrupt},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyBareStartup(test.snapshot, test.authority, test.gateway); got != test.want {
				t.Fatalf("classifyBareStartup()=%q want=%q", got, test.want)
			}
		})
	}
}

func TestDegradedUncertifiedManagerReportsExactRecertificationCommand(t *testing.T) {
	readiness := managerReadiness{authority: "ready", model: "configured: qwen3.5:4b", artifact: "installed", certification: "absent, stale, or invalid"}
	if got, want := readiness.nextStep("qwen3.5:4b"), "aegis manager certify qwen3.5-4b"; got != want {
		t.Fatalf("nextStep()=%q want=%q", got, want)
	}
}

func TestOnboardingProgressUsesVerifiedChecksForRepairStage(t *testing.T) {
	cases := []struct {
		name      string
		checks    []onboarding.Check
		completed string
		current   string
	}{
		{
			name: "authority repair preserves principal progress",
			checks: []onboarding.Check{
				{Name: "principal", Status: "verified"},
				{Name: "credential-authority", Status: "repair-required"},
			},
			completed: "1/5",
			current:   "credential authority",
		},
		{
			name: "runtime repair preserves authority progress",
			checks: []onboarding.Check{
				{Name: "principal", Status: "verified"},
				{Name: "credential-authority", Status: "verified"},
				{Name: "runtime-route", Status: "repair-required"},
			},
			completed: "2/5",
			current:   "Hermes and local Ollama runtime",
		},
		{
			name: "model repair preserves runtime progress",
			checks: []onboarding.Check{
				{Name: "principal", Status: "verified"},
				{Name: "credential-authority", Status: "verified"},
				{Name: "Hermes Agent", Status: "verified"},
				{Name: "Ollama", Status: "verified"},
				{Name: "exact-model", Status: "repair-required"},
			},
			completed: "3/5",
			current:   "exact model binding",
		},
		{
			name: "certification repair preserves model progress",
			checks: []onboarding.Check{
				{Name: "principal", Status: "verified"},
				{Name: "credential-authority", Status: "verified"},
				{Name: "Hermes Agent", Status: "verified"},
				{Name: "Ollama", Status: "verified"},
				{Name: "exact-model", Status: "verified"},
				{Name: "runtime-route", Status: "repair-required"},
			},
			completed: "4/5",
			current:   "end-to-end certification",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetOut(&output)
			renderOnboardingProgress(cmd, onboarding.Snapshot{State: onboarding.RepairRequired, Checks: test.checks})
			if !strings.Contains(output.String(), test.completed) || !strings.Contains(output.String(), test.current) {
				t.Fatalf("repair progress = %q", output.String())
			}
		})
	}
}

func TestOnboardingProgressHidesCompletedCheckRepetition(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	renderOnboardingProgress(cmd, onboarding.Snapshot{
		State:  onboarding.RuntimeConfigured,
		Reason: "model_incomplete",
		Checks: []onboarding.Check{
			{Name: "principal", Status: "verified"},
			{Name: "credential-authority", Status: "verified"},
			{Name: "Hermes Agent", Status: "verified"},
			{Name: "exact-model", Status: "incomplete", Reason: "manager_model_absent", Remedy: "aegis init"},
		},
	})
	text := output.String()
	if strings.Contains(text, "principal") || strings.Contains(text, "credential-authority") || strings.Contains(text, "Hermes Agent") {
		t.Fatalf("verified checks were repeated: %q", text)
	}
	for _, expected := range []string{"3/5", "exact model binding", "exact-model: incomplete", "manager_model_absent", "next           aegis init"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("progress missing %q: %q", expected, text)
		}
	}
}

func TestSelectInstalledCandidateSkipsOneItemMenu(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	input := newTerminalInput(strings.NewReader("next answer\n"))
	installed := []managerdomain.InstalledCandidate{{Candidate: managerdomain.Candidate{ID: "only-candidate"}}}

	renderInstalledCandidates(cmd, installed)
	selected, ok, err := selectInstalledCandidate(cmd, input, installed)
	if err != nil || !ok || selected.Candidate.ID != "only-candidate" {
		t.Fatalf("selected=%+v ok=%v error=%v", selected, ok, err)
	}
	if strings.Contains(output.String(), "[1]") || strings.Contains(output.String(), "Select") || !strings.Contains(output.String(), "selected automatically: only-candidate") {
		t.Fatalf("unexpected one-candidate output: %q", output.String())
	}
	next, eof, err := readBootstrapLine(cmd, input, 32)
	if err != nil || eof || next != "next answer" {
		t.Fatalf("automatic selection consumed next input: next=%q eof=%v error=%v", next, eof, err)
	}
}

func TestSelectInstalledCandidatePromptsForMultipleItems(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	input := newTerminalInput(strings.NewReader("2\n"))
	installed := []managerdomain.InstalledCandidate{
		{Candidate: managerdomain.Candidate{ID: "first"}},
		{Candidate: managerdomain.Candidate{ID: "second"}},
	}

	renderInstalledCandidates(cmd, installed)
	selected, ok, err := selectInstalledCandidate(cmd, input, installed)
	if err != nil || !ok || selected.Candidate.ID != "second" {
		t.Fatalf("selected=%+v ok=%v error=%v", selected, ok, err)
	}
	if !strings.Contains(output.String(), "[1] first") || !strings.Contains(output.String(), "[2] second") || !strings.Contains(output.String(), "Select one installed candidate number (no default):") {
		t.Fatalf("multiple-candidate prompt missing: %q", output.String())
	}
}
