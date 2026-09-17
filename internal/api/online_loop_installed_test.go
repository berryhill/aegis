package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Real installed CLI -> Unix peer + bearer API -> shared application -> Badger.
// This deliberately never starts a runtime, activates a Loop, or submits work.
func TestInstalledOnlineLoopPublication(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	client := unixClient(svc.Config.API.UnixSocket)
	fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "online-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "existing-source", AgentID: "existing-builder", Runtime: registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "profile/synthetic"}, Ownership: registry.Ownership{OwnerID: "synthetic-owner", AccountabilityID: "synthetic-team"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "existing-builder", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}}}})
	if err != nil {
		t.Fatal(err)
	}
	var registered struct {
		Agent app.FleetAgent `json:"agent"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/agents", app.NewRegisterFleetAgentInput(fixture, "online-fleet", "existing-source"), &registered, http.StatusCreated)
	binary := filepath.Join(t.TempDir(), "aegis")
	build := exec.Command("go", "build", "-ldflags=-X github.com/berryhill/aegis/internal/buildinfo.Version=0.0.0-online-test", "-o", binary, "./cmd/aegis")
	build.Dir = "../.."
	build.Env = append(os.Environ(), "GOMAXPROCS=2")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	cfgPath := filepath.Join(t.TempDir(), "owner.json")
	raw, _ := json.Marshal(svc.Config)
	if err := os.WriteFile(cfgPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(configPath, target string, args ...string) ([]byte, error) {
		argv := append([]string{"--config", configPath, "--target", target}, args...)
		command := exec.Command(binary, argv...)
		return command.CombinedOutput()
	}
	target := svc.Config.API.Console.Origin + "/console/agents#/agents"
	out, err := run(cfgPath, target, "agents", "list")
	if err != nil || !bytes.Contains(out, []byte("existing-builder")) {
		t.Fatalf("list: %v %s", err, out)
	}
	example, err := exec.Command(binary, "loops", "example").Output()
	if err != nil {
		t.Fatal(err)
	}
	var input app.PublishLoopInput
	if err := json.Unmarshal(example, &input); err != nil {
		t.Fatal(err)
	}
	input.AgentID = "existing-builder"
	inputPath := filepath.Join(t.TempDir(), "loop.json")
	save := func(input app.PublishLoopInput) {
		raw, _ := json.Marshal(input)
		if err := os.WriteFile(inputPath, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save(input)
	out, err = run(cfgPath, target, "loops", "validate", inputPath)
	if err != nil {
		t.Fatalf("validate: %v %s", err, out)
	}
	var validation app.ValidatedLoop
	if err := json.Unmarshal(out, &validation); err != nil || validation.Published || validation.Validation.Outcome != loop.ValidationValid {
		t.Fatalf("invalid validation readback: %v %s", err, out)
	}
	var before []app.LoopView
	apiRequest(t, client, http.MethodGet, "/v1/loops", nil, &before, http.StatusOK)
	if len(before) != 0 {
		t.Fatal("validation published a revision")
	}
	out, err = run(cfgPath, target, "loops", "publish", inputPath)
	if err != nil {
		t.Fatalf("publish: %v %s", err, out)
	}
	var published app.PublishedLoop
	if err := json.Unmarshal(out, &published); err != nil {
		t.Fatal(err)
	}
	if published.Revision.Digest == "" || published.Validation.Outcome != loop.ValidationValid {
		t.Fatalf("invalid publication: %s", out)
	}
	out, err = run(cfgPath, target, "loops", "show", "basic-implementation", "1")
	if err != nil {
		t.Fatalf("readback: %v %s", err, out)
	}
	var view app.LoopView
	if err := json.Unmarshal(out, &view); err != nil {
		t.Fatal(err)
	}
	if view.Revision.Digest != published.Revision.Digest || view.Lifecycle.State != loop.LifecycleDraft || len(view.History) != 0 {
		t.Fatalf("incorrect inactive readback: %s", out)
	}
	out, err = run(cfgPath, target, "loops", "publish", inputPath)
	if err != nil {
		t.Fatalf("replay: %v %s", err, out)
	}
	var replay app.PublishedLoop
	json.Unmarshal(out, &replay)
	if !replay.Decision.Idempotent || replay.Revision.Digest != published.Revision.Digest {
		t.Fatal("immutable replay changed")
	}
	input.Revision.Steps[0].Retry.MaxAttempts = 0
	save(input)
	if out, err := run(cfgPath, target, "loops", "validate", inputPath); err == nil {
		t.Fatalf("invalid admitted: %s", out)
	}
	if out, err := run(cfgPath, target, "loops", "publish", inputPath); err == nil {
		t.Fatalf("invalid published: %s", out)
	}
	if out, err := run(cfgPath, target, "loops", "activate", "basic-implementation", inputPath); err == nil {
		t.Fatalf("activation admitted: %s", out)
	}
	// A config pointing at the actual socket but naming another state root must
	// fail before the Agent operation and never create the alternate directory.
	wrong := svc.Config
	wrong.StateDir = filepath.Join(t.TempDir(), "absent-state")
	raw, _ = json.Marshal(wrong)
	wrongPath := filepath.Join(t.TempDir(), "wrong.json")
	os.WriteFile(wrongPath, raw, 0600)
	if out, err := run(wrongPath, target, "agents", "list"); err == nil || !bytes.Contains(out, []byte("owning_instance_mismatch")) {
		t.Fatalf("wrong config: %v %s", err, out)
	}
	if _, err := os.Stat(wrong.StateDir); !os.IsNotExist(err) {
		t.Fatal("created wrong instance state")
	}
	wrong = svc.Config
	wrong.API.Token = "unauthorized"
	raw, _ = json.Marshal(wrong)
	os.WriteFile(wrongPath, raw, 0600)
	if out, err := run(wrongPath, target, "agents", "list"); err == nil || !bytes.Contains(out, []byte("HTTP 401")) {
		t.Fatalf("unauthorized: %v %s", err, out)
	}
	var queue []app.QueueExecutionView
	apiRequest(t, client, http.MethodGet, "/v1/queue", nil, &queue, http.StatusOK)
	if len(queue) != 0 {
		t.Fatal("publication queued work")
	}
	var agents []app.FleetAgent
	apiRequest(t, client, http.MethodGet, "/v1/agents", nil, &agents, http.StatusOK)
	if len(agents) != 1 {
		t.Fatal("publication duplicated agents")
	}
}
