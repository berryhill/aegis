package api

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

// A child process closes both stores before the actual root candidate opens them.
func TestLoopAvailabilityConsoleBrowserSeed(t *testing.T) {
	root := os.Getenv("AEGIS_FILTER_SEED_ROOT")
	if root == "" {
		t.Skip("child-only browser fixture preparation")
	}
	svc := apiServiceAt(t, root)
	configureAPIFleet(t, svc)
	ctx := context.Background()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	owner := registerDoerWorkspaceAgent(t, svc, subject, "filter-owner", "filter-owner").Revision
	publish := func(id string, revision uint64, previous string) loop.LoopRevision {
		t.Helper()
		text := "hello"
		workspace := filepath.Join(root, "workspace")
		if err := os.MkdirAll(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		rev, _, err := loop.NewDoerRevision(id, revision, previous, loop.DoerContract{Task: "filter fixture", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 1})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: owner.AgentID, Revision: rev, ExpectedPreviousDigest: previous, IdempotencyKey: id + rev.Digest}); err != nil {
			t.Fatal(err)
		}
		return rev
	}
	publish("filter-published", 1, "")
	archived := publish("filter-archived", 1, "")
	if _, err := svc.SetLoopAvailabilityAs(ctx, subject, archived.LoopID, app.SetLoopAvailabilityInput{AgentID: owner.AgentID, Head: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: archived.LoopID, Revision: 1, Digest: archived.Digest}, EventID: "filter-archive", Sequence: 1, ExpectedState: loop.AvailabilityPublished, State: loop.AvailabilityArchived}); err != nil {
		t.Fatal(err)
	}
	publish(archived.LoopID, 2, archived.Digest)
	for _, revision := range []uint64{1, 2} {
		view, err := svc.GetLoopViewAs(ctx, subject, archived.LoopID, revision)
		if err != nil || view.Availability.State != loop.AvailabilityArchived {
			t.Fatalf("archived revision %d: %+v %v", revision, view.Availability, err)
		}
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	svc.Config.API.Listen = probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	svc.Config.API.UnixSocket = "aegis.sock"
	raw, err := json.Marshal(svc.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "owner.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLoopAvailabilityConsoleBrowser(t *testing.T) {
	python, candidate := os.Getenv("AEGIS_BROWSER_PYTHON"), os.Getenv("AEGIS_FILTER_CANDIDATE")
	if python == "" || candidate == "" {
		t.Skip("set AEGIS_BROWSER_PYTHON and AEGIS_FILTER_CANDIDATE for browser regression")
	}
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if candidate != filepath.Join(repo, "aegis") {
		t.Fatal("browser candidate must be the root aegis executable")
	}
	evidence := filepath.Join(repo, ".candidate", "issue334-combined")
	if err := os.MkdirAll(evidence, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(evidence, "browser-")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("private browser proof:", root)
	seed := exec.Command(os.Args[0], "-test.run=^TestLoopAvailabilityConsoleBrowserSeed$", "-test.v")
	seed.Env = append(os.Environ(), "AEGIS_FILTER_SEED_ROOT="+root)
	if output, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, output)
	}
	command := exec.Command(python, filepath.Join(repo, "scripts", "loop_filter_console_browser_test.py"), candidate, root)
	command.Env = append(os.Environ(), "AEGIS_CONSOLE_PASSWORD="+principalTestSecret())
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
}
