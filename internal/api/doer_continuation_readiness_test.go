package api

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
)

func TestApprovedDraftReadinessDoesNotProbeHelper(t *testing.T) {
	svc, controller, intent := continuationFixture(t, "proof-no-key")
	ctx := context.Background()
	draft, err := svc.ReadDoerDraftAs(ctx, intent.Requester, intent.DraftID)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Store.Save("receipts", "exact-fixture", core.Receipt{ID: "exact-fixture", CharterDigest: intent.Charter.Digest, Status: "verified"}); err != nil {
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
	if err = os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "helper-invoked")
	helper := filepath.Join(t.TempDir(), "helper")
	if err = os.WriteFile(helper, []byte("#!/bin/sh\nprintf invoked > '"+marker+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	digest, err := draft.Contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: binary, AuthorizedContracts: []string{digest}, LayaPython: helper, LayaHome: home}, svc.Config.StateDir, svc.Hermes); err != nil {
		t.Fatal(err)
	}
	portal := NewDoerContinuationPortal(svc, nil)
	review := app.DoerContinuationReview{Intent: intent, Draft: draft, Origin: svc.Config.API.Console.Origin}
	review.Digest = review.ContentDigest()
	pending := &doerPending{ID: "readiness", Review: review}
	portal.pending[pending.ID] = pending
	if _, ok := portal.ApprovedDraftReadiness(ctx, intent.Requester, intent.SessionID, draft); ok {
		t.Fatal("unsigned pending metadata admitted")
	}
	pending.ApprovalID, err = svc.ApproveDoerContinuationAs(ctx, controller, intent)
	if err != nil {
		t.Fatal(err)
	}
	before := continuationState(t, svc)
	for i := 0; i < 2; i++ {
		r, ok := portal.ApprovedDraftReadiness(ctx, intent.Requester, intent.SessionID, draft)
		if !ok || r.Reason != "helper_probe_required" || r.Helper.State != "not_checked" || r.Controller.State != "ready" || r.CanExecute {
			t.Fatalf("read overclaimed or probed: %+v %v", r, ok)
		}
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("helper invoked: %v", err)
	}
	if !reflect.DeepEqual(before, continuationState(t, svc)) {
		t.Fatal("readiness changed canonical state")
	}
	if _, ok := portal.ApprovedDraftReadiness(ctx, intent.Requester, "other-session", draft); ok {
		t.Fatal("session substitution admitted")
	}
}
