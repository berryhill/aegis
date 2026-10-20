package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Private controlled-runtime authority is issued by real public services. No
// production grant/session is imported; approval here is test-only consent.
func debuggerGraphAuthority(t *testing.T, svc *app.Service, subject core.Subject) (registry.AgentRevision, reference.DigestRef) {
	ctx := context.Background()
	charter := core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "distributed-agent", Name: "Distributed test", Revision: 1,
		Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0", Target: "aegis-owned-ephemeral"},
		Stanzas: []core.TrustStanza{{ID: "principal", Name: "Principal", Enabled: true,
			Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{SubjectIDs: []string{"local-uid:" + strconv.Itoa(os.Getuid())}, PrincipalIDs: []string{svc.Config.Principal.ID}, Issuers: []string{"linux-so-peercred"}, Environments: []string{"local"}}}, RequireFresh: true, MaxAuthAgeSec: 60},
			Grant:          core.Grant{Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}}, Scopes: core.Scopes{Memory: []string{"principal-memory"}, Credentials: []string{"provider:test"}},
			Session: core.SessionPolicy{MaximumLifetimeSec: 60, RequireReauth: true}, Approval: core.ApprovalPolicy{RequiredOperations: []string{"provision"}, MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"},
			Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "fixture-model", Provider: "test"}}}, CreatedBy: svc.Config.Principal.ID, CreatedAt: svc.Now()}

	charter.Stanzas[0].Grant = core.Grant{}
	charter.Stanzas[0].Scopes = core.Scopes{}
	charter.Stanzas[0].Hermes = core.HermesConfig{Model: "controlled-no-key", Provider: "none"}
	// Test-only executable accepts an empty provider environment. It does not
	// produce model output or qualify a managed provider.
	data, err := os.ReadFile(svc.Config.HermesExecutable)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), "[ \"${TEST_PROVIDER_KEY:-}\" = \"api-test-secret\" ] || exit 41", ":"))
	if err = os.WriteFile(svc.Config.HermesExecutable, data, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(charter)
	canonical, err := svc.ImportCharterAs(ctx, subject, raw)
	if err != nil {
		t.Fatal(err)
	}
	fixture, _ := json.Marshal(registry.CurrentFleetFixture{SchemaVersion: registry.CurrentFleetFixtureSchemaVersion, FleetID: "debug-graph-fleet", Agents: []registry.CurrentFleetAgent{{SourceID: "debug-source", AgentID: charter.AgentID, Runtime: registry.RuntimeBinding{Adapter: charter.Runtime.Adapter, Runtime: charter.Runtime.Runtime, Target: charter.Runtime.Target}, Ownership: registry.Ownership{OwnerID: "debug-owner", AccountabilityID: "debug-owner"}, Lifecycle: registry.LifecycleEnabled, Charter: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: charter.AgentID, Revision: 1, Digest: canonical.Digest}}}})
	agent, _, err := svc.RegisterFleetAgentAs(ctx, subject, app.NewRegisterFleetAgentInput(fixture, "debug-graph-fleet", "debug-source"))
	if err != nil {
		t.Fatal(err)
	}
	review, err := svc.PreviewPlanAs(ctx, subject, charter.AgentID, 1, core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := svc.RequestApprovalAs(ctx, subject, review.Plan.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.DecideApprovalAs(ctx, subject, approval.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApplyAs(ctx, subject, review.Plan.ID, approval.ID); err != nil {
		t.Fatal(err)
	}
	mandate, _, err := svc.PreviewSessionAs(ctx, subject, charter.AgentID, 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.StartSessionAs(ctx, subject, mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = svc.TerminateSessionAs(context.Background(), subject, session.ID, "debug fixture complete")
	})
	authority, err := svc.FleetCommandAuthorityAs(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	return agent.Revision, authority.Authority
}

func debuggerPublishGraph(t *testing.T, svc *app.Service, subject core.Subject, ar, lr reference.RevisionRef, authority reference.DigestRef) fleet.AcceptedSubmission {
	ctx := context.Background()
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBinary, err = filepath.EvalSymlinks(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	layaHome := t.TempDir()
	if err = os.Chmod(layaHome, 0700); err != nil {
		t.Fatal(err)
	}
	laya := filepath.Join(layaHome, "controlled-laya")
	if err = os.WriteFile(laya, []byte(`#!/bin/sh
read request
case "$request" in
  *'"kind":"gate"'*) printf '%s\n' '{"version":1,"kind":"gate","answers":{"specified":{"choice":"yes","answer_confidence":0.9},"result_defined":{"choice":"yes","answer_confidence":0.9}}}' ;;
  *'"kind":"verdict"'*) printf '%s\n' '{"version":1,"kind":"verdict","answers":{"done":{"choice":"yes","answer_confidence":0.9},"stays_in_scope":{"choice":"yes","answer_confidence":0.9},"fulfills":{"choice":"yes","answer_confidence":0.9},"works":{"choice":"yes","answer_confidence":0.9},"practices":{"choice":"yes","answer_confidence":0.9}}}' ;;
  *) exit 1 ;;
esac
`), 0700); err != nil {
		t.Fatal(err)
	}
	view, err := svc.GetLoopViewAs(ctx, subject, lr.ID, lr.Revision)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := view.Revision.Doer.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.QueueWorker.ConfigureImplementation(config.Implementation{AuthorizedContracts: []string{digest}, GoBinary: goBinary, LayaPython: laya, LayaHome: layaHome}, svc.Config.StateDir, svc.Hermes); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLoopLifecycleAs(ctx, subject, lr.ID, app.SetLoopLifecycleInput{AgentID: ar.ID, Loop: lr, State: loop.LifecycleActive, EventID: "debug-activate"}); err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishGraphAs(ctx, subject, app.PublishGraphInput{AgentID: ar.ID, Revision: graph.GraphRevision{GraphID: "debug-graph", Revision: 1, Nodes: []graph.Node{{ID: "debug-node", Participant: ar, Loop: lr}}}, IdempotencyKey: "debug-graph-publication"})
	if err != nil {
		t.Fatal(err)
	}
	exact := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: published.Revision.GraphID, Revision: 1, Digest: published.Revision.Digest}
	decision, err := svc.SubmitGraphAs(ctx, subject, app.SubmitGraphInput{Authority: authority, Graph: exact, SubmissionID: "debug-submission", IdempotencyKey: "debug-intent", SnapshotID: "debug-snapshot", QueueItemID: "debug-queue", GraphRunID: "debug-run", TransitionID: "debug-queued", RejectionID: "debug-rejection", MaxAttempts: 1})
	if err != nil || decision.Accepted == nil {
		t.Fatalf("public Graph admission: %+v %v", decision, err)
	}
	shown, err := svc.GetGraphAs(ctx, subject, exact.ID, 1)
	if err != nil || shown.Digest != exact.Digest || decision.Accepted.Snapshot.Graph != exact {
		t.Fatalf("exact Graph readback: %+v %v", shown, err)
	}
	return *decision.Accepted
}
