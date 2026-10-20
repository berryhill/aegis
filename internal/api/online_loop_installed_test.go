package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/testprocess"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
	binary := filepath.Join(t.TempDir(), "aegis")
	build := exec.Command("go", "build", "-ldflags=-X github.com/berryhill/aegis/internal/buildinfo.Version=0.0.0-online-test", "-o", binary, "./cmd/aegis")
	build.Dir = "../.."
	build.Env = append(os.Environ(), "GOMAXPROCS=2")
	if out, err := testprocess.CombinedOutput(build, 2*time.Minute); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	cfgPath := filepath.Join(t.TempDir(), "owner.json")
	raw, _ := json.Marshal(svc.Config)
	if err := os.WriteFile(cfgPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(configPath, target string, args ...string) ([]byte, error) {
		// Each CLI call performs a config read plus its operation. Respect the
		// production five-request/second limiter without changing server policy.
		time.Sleep(500 * time.Millisecond)
		argv := append([]string{"--config", configPath, "--target", target}, args...)
		command := exec.Command(binary, argv...)
		return command.CombinedOutput()
	}
	target := svc.Config.API.Console.Origin + "/console/agents#/agents"
	registrationPath := filepath.Join(t.TempDir(), "registration.json")
	registration := app.NewRegisterFleetAgentInput(fixture, "online-fleet", "existing-source")
	registrationJSON, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	saveRegistration := func(data []byte) {
		t.Helper()
		if err := os.WriteFile(registrationPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	saveRegistration(registrationJSON)
	out, err := run(cfgPath, target, "agents", "register", registrationPath)
	if err != nil {
		t.Fatalf("register: %v %s", err, out)
	}
	var registered struct {
		Agent   app.FleetAgent `json:"agent"`
		Created bool           `json:"created"`
	}
	if err := json.Unmarshal(out, &registered); err != nil || !registered.Created || registered.Agent.Revision.AgentID != "existing-builder" || registered.Agent.Revision.Revision != 1 || registered.Agent.Revision.Digest == "" {
		t.Fatalf("registration envelope: %v %s", err, out)
	}
	out, err = run(cfgPath, target, "agents", "show", "existing-builder", "1")
	var exact app.FleetAgent
	if err != nil || json.Unmarshal(out, &exact) != nil || !reflect.DeepEqual(exact, registered.Agent) {
		t.Fatalf("exact registered revision: %v %s", err, out)
	}
	out, err = run(cfgPath, target, "agents", "register", registrationPath)
	var replayRegistration struct {
		Agent   app.FleetAgent `json:"agent"`
		Created bool           `json:"created"`
	}
	if err != nil || json.Unmarshal(out, &replayRegistration) != nil || replayRegistration.Created || !reflect.DeepEqual(replayRegistration.Agent, registered.Agent) {
		t.Fatalf("registration replay: %v %s", err, out)
	}
	for name, data := range map[string][]byte{
		"malformed":        []byte(`{"fixture":`),
		"unknown":          append([]byte(`{"unknown":true,`), registrationJSON[1:]...),
		"unknown-identity": bytes.Replace(registrationJSON, []byte(`"identity":{`), []byte(`"identity":{"unknown":true,`), 1),
		"unknown-fixture":  bytes.Replace(registrationJSON, []byte(`"fixture":{`), []byte(`"fixture":{"unknown":true,`), 1),
		"trailing":         append(append([]byte(nil), registrationJSON...), []byte(` {}`)...),
	} {
		t.Run("registration-denies-"+name, func(t *testing.T) {
			saveRegistration(data)
			if out, err := run(cfgPath, target, "agents", "register", registrationPath); err == nil {
				t.Fatalf("invalid registration admitted: %s", out)
			}
		})
	}
	saveRegistration(registrationJSON)
	if out, err := run(cfgPath, "http://127.0.0.1:9999/console", "agents", "register", registrationPath); err == nil || !bytes.Contains(out, []byte("owning_instance_mismatch")) {
		t.Fatalf("foreign registration target: %v %s", err, out)
	}
	out, err = run(cfgPath, target, "agents", "list")
	if err != nil || !bytes.Contains(out, []byte("existing-builder")) {
		t.Fatalf("list: %v %s", err, out)
	}
	contract := loop.VerifiedImplementation{SchemaVersion: loop.VerifiedImplementationSchema, Task: "Implement bounded addition", Acceptance: "TestAdd passes without modifying its source", Workspace: t.TempDir(), WritableFiles: []string{"add.go"}, MaxPasses: 2, Policy: loop.GoTestPolicy{Kind: "go-test.v1", Packages: []string{"."}, RequiredTests: []loop.RequiredGoTest{{Package: "example.test/add", Name: "TestAdd"}}, TimeoutSeconds: 30}}
	authoringPath := filepath.Join(t.TempDir(), "authoring.json")
	authoring, _ := json.Marshal(map[string]any{"agent_id": "existing-builder", "loop_id": "basic-implementation", "revision": 1, "idempotency_key": "installed-implementation-v3", "implementation": contract})
	if err := os.WriteFile(authoringPath, authoring, 0600); err != nil {
		t.Fatal(err)
	}
	example, err := exec.Command(binary, "loops", "implementation", authoringPath).Output()
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
	if view.Revision.SchemaVersion != loop.ImplementationRevisionSchemaVersion || view.Revision.Digest != input.Revision.Digest || view.Revision.Digest != published.Revision.Digest || view.Lifecycle.State != loop.LifecycleDraft || len(view.History) != 0 {
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
	if out, err := run(wrongPath, target, "agents", "register", registrationPath); err == nil || !bytes.Contains(out, []byte("owning_instance_mismatch")) {
		t.Fatalf("wrong config: %v %s", err, out)
	}
	if _, err := os.Stat(wrong.StateDir); !os.IsNotExist(err) {
		t.Fatal("created wrong instance state")
	}
	wrong = svc.Config
	wrong.API.Token = "unauthorized"
	raw, _ = json.Marshal(wrong)
	os.WriteFile(wrongPath, raw, 0600)
	if out, err := run(wrongPath, target, "agents", "register", registrationPath); err == nil || !bytes.Contains(out, []byte("HTTP 401")) {
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
