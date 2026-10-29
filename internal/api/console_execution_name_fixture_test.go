package api

import (
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
)

const executionFixtureName = `Review <changes> & "ship"`

// Controlled durable fixture only: no provider, live grants or production state.
// A separate child closes the stores before the frozen executable opens them.
// Run via acceptance_port.py; AEGIS_EXECUTION_NAME_ROOT selects private evidence.
func TestExecutionLoopNameSeed(t *testing.T) {
	root := os.Getenv("AEGIS_EXECUTION_NAME_ROOT")
	if root == "" {
		t.Skip("isolated installed-list fixture")
	}
	t.Setenv("AEGIS_DEBUGGER_SEED_ROOT", root)
	svc, subject, _, executor, done := debuggerFixtureNamed(t, false, executionFixtureName)
	close(executor.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	head, err := svc.GetLoopViewAs(ctx, subject, "debug-loop", 2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SetLoopAvailabilityAs(ctx, subject, "debug-loop", app.SetLoopAvailabilityInput{AgentID: "debug-agent", Head: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "debug-loop", Revision: 2, Digest: head.Revision.Digest}, EventID: "execution-name-archive", Sequence: 1, ExpectedState: loop.AvailabilityPublished, State: loop.AvailabilityArchived})
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AEGIS_ACCEPTANCE_PORT") == "" {
		t.Fatal("shared port custody required")
	}
	svc.Config.API.Listen = "127.0.0.1:" + os.Getenv("AEGIS_ACCEPTANCE_PORT")
	svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	svc.Config.API.UnixSocket = "aegis.sock"
	raw, err := json.Marshal(svc.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "owner.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

// Exercises real password authentication, the routed list and archived inventory
// rather than merely invoking the naming projection with a hand-populated list.
func TestExecutionLoopNameAuthenticated(t *testing.T) {
	port := os.Getenv("AEGIS_ACCEPTANCE_PORT")
	if port == "" {
		t.Skip("run through shared acceptance_port.py lifetime custody")
	}
	svc, subject, _, executor, done := debuggerFixtureNamed(t, false, executionFixtureName)
	close(executor.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	head, err := svc.GetLoopViewAs(ctx, subject, "debug-loop", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SetLoopAvailabilityAs(ctx, subject, "debug-loop", app.SetLoopAvailabilityInput{AgentID: "debug-agent", Head: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "debug-loop", Revision: 2, Digest: head.Revision.Digest}, EventID: "name-archive", Sequence: 1, ExpectedState: loop.AvailabilityPublished, State: loop.AvailabilityArchived}); err != nil {
		t.Fatal(err)
	}
	svc.Config.API.Listen = "127.0.0.1:" + port
	svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	runctx, cancel := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- Serve(runctx, svc) }()
	defer func() {
		cancel()
		if err := <-stopped; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "tcp", svc.Config.API.Listen)
	client, _ := loginConsole(t, svc.Config.API.Listen)

	for _, path := range []string{"/console/queue?limit=1", "/console/fragments/surface?domain=queue&limit=1"} {
		body := queueScopeGet(t, client, svc.Config.API.Console.Origin, path, http.StatusOK)
		if !strings.Contains(body, `Review &lt;changes&gt; &amp; &#34;ship&#34;`) || strings.Contains(body, `<strong>New renamed Loop</strong>`) || !strings.Contains(body, "record-debug-queue") {
			t.Fatalf("pinned archived name/identity missing: %s", body)
		}
	}
}

func TestExecutionLoopNameBrowser(t *testing.T) {
	if os.Getenv("AEGIS_EXECUTION_NAME_CANDIDATE") == "" {
		t.Skip("set frozen candidate and evidence root; run through acceptance_port.py")
	}
	root := os.Getenv("AEGIS_EXECUTION_NAME_ROOT")
	if root == "" {
		t.Fatal("private evidence root required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestExecutionLoopNameSeed$", "-test.v")
	child.Env = os.Environ()
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}
	runner := exec.Command("python3", "../../scripts/execution_name_browser_test.py")
	runner.Env = append(os.Environ(), "AEGIS_CONSOLE_PASSWORD="+principalTestSecret())
	out, err := runner.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
}
