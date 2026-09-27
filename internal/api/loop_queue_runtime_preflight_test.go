package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestQueueDoerUnavailableRuntimeDoesNotActivateOrSubmit(t *testing.T) {
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
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "runtime-doer-agent", Name: "Runtime Doer", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{svc.Config.Principal.ID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{}, Scopes: core.Scopes{}, Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true},
			Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"},
			Hermes: core.HermesConfig{Model: "proof-no-key", Provider: "none"}}}, CreatedBy: svc.Config.Principal.ID, CreatedAt: svc.Now()}
	wire, err := json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := svc.ImportCharterAs(ctx, subject, wire)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "runtime-doer-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "runtime-doer-source", AgentID: charter.AgentID,
		Runtime:   registry.RuntimeBinding{Adapter: charter.Runtime.Adapter, Runtime: charter.Runtime.Runtime, Target: charter.Runtime.Target},
		Ownership: registry.Ownership{OwnerID: "doer-owner", AccountabilityID: "doer-owner"}, Lifecycle: registry.LifecycleEnabled,
		Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: charter.AgentID, Revision: 1, Digest: canonical.Digest}}}})
	if err != nil {
		t.Fatal(err)
	}
	var registered struct {
		Agent app.FleetAgent `json:"agent"`
	}
	apiRequest(t, client, http.MethodPost, "/v1/agents", app.NewRegisterFleetAgentInput(fixture, "runtime-doer-fleet", "runtime-doer-source"), &registered, http.StatusCreated)
	workspace := t.TempDir()
	contract := loop.DoerContract{Task: "Create result.txt", Workspace: workspace, WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 1}
	revision, _, err := loop.NewDoerRevision("doer-runtime-unavailable", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: charter.AgentID, Revision: revision, IdempotencyKey: "runtime-doer-publish"})
	if err != nil {
		t.Fatal(err)
	}
	var review core.Review
	apiRequest(t, client, http.MethodPost, "/v1/plans/preview", map[string]any{"agent": charter.AgentID, "revision": 1, "environment": core.Environment{Name: "local"}}, &review, http.StatusCreated)
	var approval core.Approval
	apiRequest(t, client, http.MethodPost, "/v1/approvals", map[string]any{"plan_id": review.Plan.ID, "ttl": "1m"}, &approval, http.StatusCreated)
	apiRequest(t, client, http.MethodPost, "/v1/approvals/"+approval.ID+"/decision", map[string]bool{"approve": true}, &approval, http.StatusOK)
	var receipt core.Receipt
	apiRequest(t, client, http.MethodPost, "/v1/provision", map[string]string{"plan_id": review.Plan.ID, "approval_id": approval.ID}, &receipt, http.StatusCreated)
	if receipt.Status != "verified" {
		t.Fatalf("fixture provisioning was not verified: %s", receipt.Status)
	}
	runtimeExecutable, err := os.ReadFile(svc.Config.HermesExecutable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(svc.Config.HermesExecutable, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	agent := registered.Agent.Revision
	input := app.QueueLoopInput{Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: agent.AgentID, Revision: agent.Revision, Digest: agent.Digest},
		Loop:           reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: revision.Revision, Digest: published.Revision.Digest},
		IdempotencyKey: "runtime-doer-run", Activate: true}
	result, err := svc.QueueLoopAs(ctx, subject, input)
	if err != nil || result.Reason != "runtime_unavailable_or_unsupported" || result.QueueItemID != "" || result.Execution != nil {
		t.Fatalf("unavailable runtime reached execution mutation: %+v, err %v", result, err)
	}
	if err := os.WriteFile(svc.Config.HermesExecutable, runtimeExecutable, 0700); err != nil {
		t.Fatal(err)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBinary, err = filepath.EvalSymlinks(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	silentLaya := filepath.Join(t.TempDir(), "silent-python")
	if err := os.WriteFile(silentLaya, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	digest, err := contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	layaHome := t.TempDir()
	if err := os.Chmod(layaHome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := svc.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: goBinary, AuthorizedContracts: []string{digest}, LayaPython: silentLaya, LayaHome: layaHome}, svc.Config.StateDir, svc.Hermes); err != nil {
		t.Fatal(err)
	}
	if err := svc.QueueWorker.ValidateLoopAdmission(published.Revision, agent); err != nil {
		t.Fatalf("configured Doer fixture is structurally invalid: %v", err)
	}
	input.IdempotencyKey = "runtime-doer-silent-laya-run"
	result, err = svc.QueueLoopAs(ctx, subject, input)
	if err != nil || result.Reason != "local_laya_unavailable" || result.QueueItemID != "" || result.Execution != nil {
		t.Fatalf("silent Laya reached execution mutation: %+v, err %v", result, err)
	}
	view, err := svc.GetLoopViewAs(ctx, subject, revision.LoopID, 1)
	if err != nil || view.Lifecycle.State != loop.LifecycleDraft || len(view.History) != 0 {
		t.Fatalf("unavailable runtime activated Loop: %+v, err %v", view.Lifecycle, err)
	}
	graphs, err := svc.ListGraphsAs(ctx, subject)
	if err != nil || len(graphs) != 0 {
		t.Fatalf("unavailable runtime published Graph: %d, err %v", len(graphs), err)
	}
	items, err := svc.ListQueueAs(ctx, subject)
	if err != nil || len(items) != 0 {
		t.Fatalf("unavailable runtime submitted Queue: %d, err %v", len(items), err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
		t.Fatalf("unavailable runtime wrote selected file: %v", err)
	}
}
