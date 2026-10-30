package command

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/slash"
)

func assertContributionCandidatePreflight(t *testing.T, ctx context.Context, h *contributionHTTPFixture, s *app.Service, id, token string, ref reference.RevisionRef, url string) {
	t.Helper()
	client, err := newGatewayManagerClient(s.Config)
	if err != nil {
		t.Fatal(err)
	}
	client.sessionID, client.token = id, token
	absent, err := client.execute(ctx, "/contribute readiness")
	if err != nil {
		t.Fatal(err)
	}
	readiness := decodeContributionReadiness(t, absent)
	if readiness.Profile.Reason != "contribution_profile_absent" || readiness.Service != app.CurrentContributionServiceIdentity() || readiness.PreparationEligible || absent.Data["execution_started"] != false {
		t.Fatalf("root absent preflight: %+v", readiness)
	}
	wrong := app.CurrentContributionServiceIdentity()
	wrong.ExecutableDigest = "sha256:" + strings.Repeat("f", 64)
	var denied slash.Result
	err = h.call(ctx, "/v1/manager/sessions/"+id+"/commands", token, map[string]any{"input": "/contribute run " + url, "expected_service": wrong}, &denied)
	if err != nil || denied.Reason != "candidate_service_mismatch" || denied.Data["execution_started"] != false {
		t.Fatalf("mismatched root/service: %+v %v", denied, err)
	}
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil || len(items) != 0 {
		t.Fatal("mismatch created invocation", err)
	}
	selected, err := client.execute(ctx, "/contribute select "+ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	readiness = decodeContributionReadiness(t, selected)
	if readiness.Loop != ref || readiness.Native.Reason != "exact_contribution_native_scope_required" || len(readiness.Roles) != 3 || readiness.NativeScope == nil || readiness.PreparationEligible {
		t.Fatalf("selected host preflight: %+v", readiness)
	}
	for _, role := range readiness.Roles {
		if role.Reference.State != "ready" || role.Selection.State != "ready" || role.Provisioning.State != "ready" {
			t.Fatalf("role preflight: %+v", role)
		}
	}
}
func decodeContributionReadiness(t *testing.T, result slash.Result) app.ContributionReadiness {
	t.Helper()
	b, err := json.Marshal(result.Data["readiness"])
	if err != nil {
		t.Fatal(err)
	}
	var r app.ContributionReadiness
	if err = json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}
func assertContributionHostDenial(t *testing.T, ctx context.Context, h *contributionHTTPFixture, s *app.Service, id, token string, ref reference.RevisionRef, url, workspace string) {
	t.Helper()
	client, err := newGatewayManagerClient(s.Config)
	if err != nil {
		t.Fatal(err)
	}
	client.sessionID, client.token = id, token
	for i := 0; i < 2; i++ {
		result, err := client.execute(ctx, "/contribute run "+url)
		if err != nil || result.State != "blocked" || result.Reason != "exact_contribution_native_scope_required" || result.Data["execution_started"] != false {
			t.Fatalf("host denial: %+v %v", result, err)
		}
	}
	items, err := s.FleetRepository.ListQueueItems(ctx)
	if err != nil || len(items) != 1 {
		t.Fatal("denial duplicated retained invocation", err)
	}
	authorities, err := s.Authority.ListAuthorityContexts(ctx)
	if err != nil || len(authorities) != 0 {
		t.Fatal("host denial launched role", err)
	}
	if _, err = os.Lstat(workspace); !os.IsNotExist(err) {
		t.Fatal("host denial created worktree", err)
	}
	opened, err := h.Open(ctx, core.Subject{})
	if err != nil {
		t.Fatal(err)
	}
	key := app.ContributionIntentKey(ref, url, "")
	result, err := h.Execute(ctx, core.Subject{}, opened.ID, opened.Token, "/contribute status "+key)
	if err != nil {
		t.Fatal(err)
	}
	if result.Data["run_id"] != items[0].GraphRunID {
		t.Fatal("fresh session lost same retained intent")
	}
	wire, err := json.Marshal(result.Data["execution"])
	if err != nil {
		t.Fatal(err)
	}
	var execution app.ContributionExecutionResult
	if err = json.Unmarshal(wire, &execution); err != nil {
		t.Fatal(err)
	}
	if execution.Provenance != nil {
		t.Fatal("failed preflight pinned an execution image")
	}
}
