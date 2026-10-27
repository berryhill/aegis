package api

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
)

// Public observation retains its own deadline even when the real helper can
// answer successfully just beyond it. No Run marker or execution is used here.
func TestDoerCandidateSlowPublicProbeRemainsBounded(t *testing.T) {
	t.Parallel()
	s, subject, input := candidateReadinessFixture(t, "proof-no-key")
	ctx := context.Background()
	r, err := s.ReadDoerCandidateReadinessAs(ctx, subject, input)
	if err != nil || r.RequiredCharter == nil {
		t.Fatalf("fixture: %+v %v", r, err)
	}
	if err := s.Store.Save("receipts", "slow-probe-fixture", core.Receipt{ID: "slow-probe-fixture", CharterDigest: r.RequiredCharter.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	binary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(t.TempDir(), "helper")
	response := `{"version":1,"kind":"gate","answers":{"specified":{"choice":"yes","answer_confidence":0.9},"result_defined":{"choice":"yes","answer_confidence":0.9}}}`
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nsleep 11 >/dev/null 2>&1 &\nwait\nprintf '%s\\n' '"+response+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: binary, AuthorizedContracts: []string{r.ContractDigest}, LayaPython: helper, LayaHome: home}, s.Config.StateDir, s.Hermes); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	r, err = s.ProbeDoerCandidateReadinessAs(ctx, subject, input)
	elapsed := time.Since(start)
	if err != nil || r.CanExecute || r.Controller.State != "ready" || r.Helper.State != "blocked" || r.Reason != "local_laya_unavailable" || elapsed < 10*time.Second {
		t.Fatalf("public probe not bounded: %+v elapsed=%s err=%v", r, elapsed, err)
	}
	t.Logf("public inspection blocked slow helper after %s", elapsed)
	// Exercise the real typed owning-service route as well: it does not call
	// the exported Go probe method and must enforce the same deadline.
	serveCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- Serve(serveCtx, s) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", s.Config.API.UnixSocket)
	var observed app.DoerCandidateReadiness
	client := unixClient(s.Config.API.UnixSocket)
	// The generic fixture client has a five-second timeout, shorter than the
	// public probe itself. Let the service's actual ten-second deadline win.
	client.Timeout = 30 * time.Second
	apiRequest(t, client, http.MethodPost, "/v1/loops/doer/readiness", app.DoerReadinessInput{Agent: input.Agent, Candidate: input.Candidate, Probe: true}, &observed, http.StatusOK)
	// The helper returns a valid success after eleven seconds. A blocked
	// helper (rather than a wall-clock upper bound) proves cancellation won.
	if observed.CanExecute || observed.Controller.State != "ready" || observed.Helper.State != "blocked" || observed.Reason != "local_laya_unavailable" {
		t.Fatalf("typed public probe escaped deadline: %+v", observed)
	}
	items, err := s.ListQueueAs(ctx, subject)
	if err != nil || len(items) != 0 {
		t.Fatalf("inspection created Queue work: %+v %v", items, err)
	}
	loops, err := s.ListLoopsAs(ctx, subject)
	if err != nil || len(loops) != 0 {
		t.Fatalf("inspection published candidate: %+v %v", loops, err)
	}
	if _, err := os.Stat(filepath.Join(input.Candidate.Doer.Workspace, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("inspection wrote result: %v", err)
	}
}
