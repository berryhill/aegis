package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Preparation runs in a child; both stores close before the root candidate serves.
func TestLoopDetailFrozenSeed(t *testing.T) {
	root := os.Getenv("AEGIS_LOOP_DETAIL_ROOT")
	if root == "" {
		t.Skip("isolated browser seed")
	}
	if os.Getenv("AEGIS_ACCEPTANCE_PORT") == "" {
		t.Fatal("lifetime port custody required")
	}
	svc := apiServiceAt(t, root)
	configureAPIFleet(t, svc)
	ctx := context.Background()
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	owner := registerDoerWorkspaceAgent(t, svc, subject, "loop-detail-owner", "loop-detail-owner").Revision
	workspace := filepath.Join(root, "workspace")
	if err = os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	revision, _, err := loop.NewDoerRevision("loop-detail", 1, "", loop.DoerContract{Task: "Synthetic Console definition fixture; never execute", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	revision.Name = "Authored Loop detail fixture"
	revision, _, err = loop.NewRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: owner.AgentID, Revision: revision, IdempotencyKey: "loop-detail-fixture"}); err != nil {
		t.Fatal(err)
	}
	metadata := []map[string]any{}
	record := func(value loop.LoopRevision, kind string) {
		t.Helper()
		view, readErr := svc.GetLoopViewAs(ctx, subject, value.LoopID, value.Revision)
		if readErr != nil {
			t.Fatal(readErr)
		}
		ids := make([]string, 0, len(view.Revision.Steps))
		for _, step := range view.Revision.Steps {
			ids = append(ids, step.ID)
		}
		metadata = append(metadata, map[string]any{"id": view.Revision.LoopID, "revision": view.Revision.Revision, "digest": view.Revision.Digest, "name": view.Revision.Name, "steps": ids, "transitions": view.Revision.Transitions, "kind": kind})
	}
	publish := func(value loop.LoopRevision, kind string) {
		t.Helper()
		_, publishErr := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: owner.AgentID, Revision: value, ExpectedPreviousDigest: value.PreviousDigest, IdempotencyKey: fmt.Sprintf("loop-detail-additional-%d", len(metadata))})
		if publishErr != nil {
			t.Fatal(publishErr)
		}
		record(value, kind)
	}
	record(revision, "doer")
	newer, _, err := loop.NewDoerRevision(revision.LoopID, 2, revision.Digest, *revision.Doer)
	if err != nil {
		t.Fatal(err)
	}
	newer.Name = "Newer authored Loop detail fixture"
	newer, _, err = loop.NewRevision(newer)
	if err != nil {
		t.Fatal(err)
	}
	publish(newer, "doer-latest")
	branch, _ := loopDetailBranchFixture(t)
	publish(branch, "branch")
	for _, value := range loopDetailCycleFixtures(t) {
		publish(value, value.LoopID)
	}
	maximum, _ := loopMaximumPresentationFixture(t)
	publish(maximum, "maximum")
	namespace := branch
	namespace.LoopID, namespace.Name = "team/loop:stage", "Namespace identifier fixture"
	namespace, _, err = loop.NewRevision(namespace)
	if err != nil {
		t.Fatal(err)
	}
	publish(namespace, "namespace")
	// Canonical invalid/terminal-only definitions are rejected, not inserted as
	// counterfeit published records just to obtain a screenshot.
	invalid := loop.LoopRevision{LoopID: "terminal-only", Revision: 1, EntryStepID: "done", Steps: []loop.Step{{ID: "done", Kind: loop.StepTerminal, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}}}}
	_, invalidValidation, invalidErr := loop.NewRevision(invalid)
	if invalidErr == nil || invalidValidation.Outcome != loop.ValidationInvalid {
		t.Fatal("terminal-only fixture must fail canonical validation")
	}
	if _, publishErr := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: owner.AgentID, Revision: invalid, IdempotencyKey: "invalid-terminal-only"}); publishErr == nil {
		t.Fatal("invalid terminal-only definition published")
	}
	inventory, inventoryErr := svc.ListLoopInventoryAs(ctx, subject)
	if inventoryErr != nil {
		t.Fatal(inventoryErr)
	}
	for _, view := range inventory {
		if view.Revision.LoopID == invalid.LoopID {
			t.Fatal("rejected definition left a published record")
		}
	}
	manifest, marshalErr := json.Marshal(map[string]any{"records": metadata, "invalid_terminal_only": invalidValidation.Issues, "invalid_record_absent": true})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err = os.WriteFile(filepath.Join(root, "fixtures.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	svc.Config.API.Listen = "127.0.0.1:" + os.Getenv("AEGIS_ACCEPTANCE_PORT")
	svc.Config.API.Console.Origin = "http://" + svc.Config.API.Listen
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	svc.Config.API.UnixSocket = filepath.Join(repo, ".scratch", ".loop-"+os.Getenv("AEGIS_ACCEPTANCE_PORT")+".sock")
	svc.Config.Credentials.ProviderAuth = nil
	raw, err := json.Marshal(svc.Config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "owner.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLoopDetailFrozenBrowser(t *testing.T) {
	if os.Getenv("AEGIS_LOOP_DETAIL_CANDIDATE") == "" {
		t.Skip("requires built candidate and claimed port")
	}
	root := os.Getenv("AEGIS_LOOP_DETAIL_ROOT")
	if root == "" || os.Getenv("AEGIS_ACCEPTANCE_PORT") == "" {
		t.Fatal("private root and claimed port required")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("fixture root must be new")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	seed := exec.Command(os.Args[0], "-test.run=^TestLoopDetailFrozenSeed$", "-test.v")
	seed.Env = os.Environ()
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed: %v %s", err, out)
	}
	python := os.Getenv("AEGIS_LOOP_DETAIL_BROWSER_PYTHON")
	if python == "" {
		python = "python3"
	}
	browser := exec.Command(python, "../../scripts/loop_detail_console_browser_test.py")
	browser.Env = append(os.Environ(), "AEGIS_CONSOLE_PASSWORD="+principalTestSecret())
	if out, err := browser.CombinedOutput(); err != nil {
		t.Fatalf("browser: %v %s", err, out)
	} else {
		t.Log(string(out))
	}
}
