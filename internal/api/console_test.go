package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/orchestration"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestBrowserHandoffConfirmationIsRestrictedToExactLoopbackCapability(t *testing.T) {
	valid := "http://127.0.0.1:34803/confirmed/" + strings.Repeat("a", 43)
	if got, err := validateBrowserHandoff(valid, "127.0.0.1"); err != nil || got != valid {
		t.Fatalf("valid browser handoff=%q err=%v", got, err)
	}
	for name, raw := range map[string]string{
		"empty":         "",
		"remote":        "http://example.test:34803/confirmed/" + strings.Repeat("a", 43),
		"host mismatch": "http://localhost:34803/confirmed/" + strings.Repeat("a", 43),
		"wrong scheme":  "https://127.0.0.1:34803/confirmed/" + strings.Repeat("a", 43),
		"missing port":  "http://127.0.0.1/confirmed/" + strings.Repeat("a", 43),
		"wrong path":    "http://127.0.0.1:34803/handoff/" + strings.Repeat("a", 43),
		"short token":   "http://127.0.0.1:34803/confirmed/short",
		"query":         valid + "?authority=admin",
		"fragment":      valid + "#authority",
		"user info":     "http://operator@127.0.0.1:34803/confirmed/" + strings.Repeat("a", 43),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateBrowserHandoff(raw, "127.0.0.1"); err == nil {
				t.Fatalf("unsafe browser handoff accepted: %q", raw)
			}
		})
	}
}

func TestConsoleFormDecoderAcceptsOneExactBoundedField(t *testing.T) {
	valid := httptest.NewRequest("POST", "/console/session", strings.NewReader("bootstrap=single-use%2Btoken"))
	valid.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	value, err := decodeConsoleForm(valid, "bootstrap")
	if err != nil || value != "single-use+token" {
		t.Fatalf("valid native form value=%q err=%v", value, err)
	}

	for name, request := range map[string]*http.Request{
		"wrong content type": httptest.NewRequest("POST", "/console/session", strings.NewReader("bootstrap=value")),
		"unknown field":      httptest.NewRequest("POST", "/console/session", strings.NewReader("bootstrap=value&authority=admin")),
		"duplicate field":    httptest.NewRequest("POST", "/console/session", strings.NewReader("bootstrap=one&bootstrap=two")),
		"oversized":          httptest.NewRequest("POST", "/console/session", bytes.NewReader(bytes.Repeat([]byte("x"), 8193))),
	} {
		t.Run(name, func(t *testing.T) {
			if name != "wrong content type" {
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			if _, err := decodeConsoleForm(request, "bootstrap"); err == nil {
				t.Fatal("unsafe native form accepted")
			}
		})
	}
}

func TestConsoleSignalsAreStrictAndPresentationOnly(t *testing.T) {
	for name, raw := range map[string]string{
		"authority": `{"authority":"admin"}`,
		"trailing":  `{"csrf":"ok"}{}`,
		"oversized": `{"csrf":"` + strings.Repeat("x", 9000) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/console/fragments/surface?datastar="+url.QueryEscape(raw), nil)
			if err := validateConsoleSignals(request); err == nil {
				t.Fatal("unsafe signals accepted")
			}
		})
	}
	request := httptest.NewRequest("GET", "/console/fragments/surface?datastar="+url.QueryEscape(`{"csrf":"presentation-only"}`), nil)
	if err := validateConsoleSignals(request); err != nil {
		t.Fatalf("closed presentation signals denied: %v", err)
	}
}

func TestConsoleDomainAndRecordSelectorsFailClosed(t *testing.T) {
	for _, raw := range []string{"authority", "../agents", "agents&stanza=admin"} {
		if _, err := parseConsoleDomain(raw); err == nil {
			t.Fatalf("forged domain %q accepted", raw)
		}
	}
	model, err := consoleSurfaceModel(app.FleetSurface{Readiness: map[string]app.SurfaceReadiness{"registry": {State: "empty", Authoritative: true}}}, consoleAgents)
	if err != nil {
		t.Fatal(err)
	}
	if model.State != "empty" {
		t.Fatalf("authoritative empty state=%q", model.State)
	}
	for _, raw := range []string{"", "-1", "0", "stanza-admin"} {
		if err = selectConsoleRecord(&model, raw); err == nil {
			t.Fatalf("forged record selector %q accepted", raw)
		}
	}
}

func TestAgentRegistryFiltersAreBoundedStableAndPresentationOnly(t *testing.T) {
	model := consoleweb.SurfaceModel{
		Domain: string(consoleAgents), State: "ready", TotalRecords: 3,
		Records: []consoleweb.RecordModel{
			{Key: "office", Label: "office", Runtime: "hermes-local", Source: "fleet-a / hermes-profile", Owner: "principal-1", Lifecycle: "enabled"},
			{Key: "reviewer", Label: "reviewer", Runtime: "hermes-remote", Source: "fleet-a / fixture", Owner: "principal-2", Lifecycle: "disabled"},
			{Key: "retired", Label: "retired", Runtime: "hermes-local", Source: "fleet-b / fixture", Owner: "principal-1", Lifecycle: "retired"},
		},
	}
	if err := filterConsoleAgents(&model, "principal-1", "enabled"); err != nil {
		t.Fatal(err)
	}
	if len(model.Records) != 1 || model.Records[0].Key != "office" || model.Query != "principal-1" || model.Lifecycle != "enabled" {
		t.Fatalf("unexpected filtered Registry model: %+v", model)
	}
	if err := selectConsoleRecord(&model, "office"); err != nil || model.Inspector == nil || model.Inspector.Key != "office" {
		t.Fatalf("stable Agent selector failed: inspector=%+v err=%v", model.Inspector, err)
	}
	for _, input := range []struct{ query, lifecycle string }{
		{strings.Repeat("x", 129), "all"}, {"office\nadmin", "all"}, {"", "unknown"},
	} {
		candidate := consoleweb.SurfaceModel{Records: model.Records}
		if err := filterConsoleAgents(&candidate, input.query, input.lifecycle); err == nil {
			t.Fatalf("unsafe Registry filter accepted: %+v", input)
		}
	}
}

func TestConsoleSurfacePreservesContextualReadinessAndCredentialMetadata(t *testing.T) {
	denied, err := consoleSurfaceModel(app.FleetSurface{
		Readiness: map[string]app.SurfaceReadiness{
			"registry": {State: "denied", ReasonCode: "collection_read_denied", Source: "fleet.agent_registrations"},
		},
		Actions: map[string]orchestration.Readiness{
			"register_fleet_agent": {
				Action:        orchestration.FleetActionRegister,
				State:         orchestration.ReadinessDenied,
				ReasonCode:    "principal_not_authorized",
				RepairActions: []orchestration.RepairAction{orchestration.RepairAuthenticate},
			},
		},
	}, consoleAgents)
	if err != nil {
		t.Fatal(err)
	}
	if denied.State != "denied" || denied.ReasonCode != "collection_read_denied" || denied.Source != "fleet.agent_registrations" || strings.Contains(denied.Status, "0 record") {
		t.Fatalf("denied readiness was collapsed or asserted a count: %+v", denied)
	}
	if len(denied.Actions) != 1 || denied.Actions[0].Key != "register_fleet_agent" || denied.Actions[0].State != "denied" || denied.Actions[0].ReasonCode != "principal_not_authorized" || len(denied.Actions[0].RepairActions) != 1 || denied.Actions[0].RepairActions[0] != "authenticate_principal" || !denied.Actions[0].Primary {
		t.Fatalf("contextual action readiness was not preserved: %+v", denied.Actions)
	}

	credentials, err := consoleSurfaceModel(app.FleetSurface{
		Credentials: []app.CredentialView{{ID: "github", Type: "environment"}},
		Readiness: map[string]app.SurfaceReadiness{
			"credentials": {State: "ready", ReasonCode: "collection_read_succeeded", Source: "config.credentials.provider_auth", Count: 1, Authoritative: true},
		},
	}, consoleCredentials)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Title != "Credentials" || credentials.State != "ready" || len(credentials.Records) != 1 || credentials.Records[0].Label != "github · environment binding" {
		t.Fatalf("credential surface=%+v", credentials)
	}
	if strings.Contains(credentials.Records[0].JSON, "source_env") || strings.Contains(credentials.Records[0].JSON, "target_env") {
		t.Fatalf("credential surface exposed custody details: %s", credentials.Records[0].JSON)
	}
}

func TestConsoleGraphRecordPreservesExactTopologyLifecycleAndSubmissionTruth(t *testing.T) {
	digest := func(marker string) string { return "sha256:" + strings.Repeat(marker, 64) }
	revision := graph.GraphRevision{
		GraphID: "graph-review", Revision: 4, Digest: digest("a"), PreviousDigest: digest("b"),
		Inputs: []graph.Port{{ID: "brief", Type: graph.TypeString, Required: true}}, Outputs: []graph.Port{{ID: "artifact", Type: graph.TypeArtifact, Required: true}},
		Nodes:          []graph.Node{{ID: "review", Participant: reference.RevisionRef{ID: "agent-reviewer", Revision: 2, Digest: digest("c")}, Loop: reference.RevisionRef{ID: "loop-review", Revision: 7, Digest: digest("d")}, Inputs: []graph.Port{{ID: "brief", Type: graph.TypeString, Required: true}}, Outputs: []graph.Port{{ID: "draft", Type: graph.TypeString, Required: true}}}, {ID: "publish", Participant: reference.RevisionRef{ID: "agent-publisher", Revision: 3, Digest: digest("e")}, Loop: reference.RevisionRef{ID: "loop-publish", Revision: 8, Digest: digest("f")}, Inputs: []graph.Port{{ID: "draft", Type: graph.TypeString, Required: true}}, Outputs: []graph.Port{{ID: "artifact", Type: graph.TypeArtifact, Required: true}}}},
		Dependencies:   []graph.Dependency{{ID: "review-before-publish", FromNodeID: "review", ToNodeID: "publish", Mappings: []graph.PortMapping{{FromPort: "draft", ToPort: "draft"}}}},
		AdmissionRules: []graph.AdmissionRule{{ID: "operator", PolicyRef: reference.DigestRef{ID: "policy-operator", Digest: digest("1")}}},
	}
	snapshot := graph.GraphRunSnapshot{SnapshotID: "snapshot-1", Digest: digest("2"), Graph: reference.RevisionRef{ID: revision.GraphID, Revision: revision.Revision, Digest: revision.Digest}, Inputs: []graph.NormalizedInput{{PortID: "brief", Type: graph.TypeString, Value: []byte(`"inspect"`)}}}
	submission := queue.Submission{SubmissionID: "submission-1", Digest: digest("3"), Authority: reference.DigestRef{ID: "authority-1", Digest: digest("4")}, MandateID: "mandate-1", Runtime: "hermes-agent"}
	item := queue.Item{ItemID: "item-1", Digest: digest("5")}
	run := execution.GraphRun{GraphRunID: "run-1"}
	view := app.GraphView{Revision: revision, Validations: []graph.GraphValidationResult{{Outcome: graph.ValidationValid, Digest: digest("6")}}, Lifecycle: graph.Lifecycle{GraphID: revision.GraphID, State: graph.LifecycleActive, ActiveRevision: revision.Revision, ActiveDigest: revision.Digest}, Runs: []app.AcceptedGraphRunView{{Snapshot: snapshot, Submission: submission, QueueItem: item, GraphRun: run}}}
	history := app.SubmissionHistory{Rejected: []queue.Rejection{{SubmissionID: "submission-rejected", ReasonCode: "invalid_input", Reason: "brief is required"}}}

	record := consoleGraphRecord(view, history, `{"graph_id":"graph-review"}`)
	if record.Key != "graph-review:4" || record.Lifecycle != string(graph.LifecycleActive) || record.Graph == nil || len(record.Graph.Nodes) != 2 || len(record.Graph.Edges) != 1 {
		t.Fatalf("Graph topology or lifecycle lost: %+v", record)
	}
	if record.Graph.Nodes[0].Loop != exactRevisionLabel("loop-review", 7, digest("d")) || record.Graph.Nodes[0].Inputs != "brief:string required=true" || record.Graph.Edges[0].Mappings != "draft → draft" {
		t.Fatalf("exact Loop binding or interface lost: %+v", record.Graph)
	}
	if len(record.Graph.AcceptedRuns) != 1 || record.Graph.AcceptedRuns[0].Snapshot != "snapshot-1 @ "+digest("2") || record.Graph.AcceptedRuns[0].Authority != "authority-1 @ "+digest("4") || !strings.Contains(record.Graph.AcceptedRuns[0].Inputs, `brief (string) = "inspect"`) {
		t.Fatalf("immutable accepted snapshot lost: %+v", record.Graph.AcceptedRuns)
	}
	if len(record.Graph.RejectedSubmissions) != 1 || record.Graph.RejectedSubmissions[0].Label != "submission-rejected" || !strings.Contains(record.Graph.RejectedSubmissions[0].Value, "invalid_input") {
		t.Fatalf("fleet-wide rejected history lost: %+v", record.Graph.RejectedSubmissions)
	}
}

func TestConsoleRenderIsBoundedAndCancellationAware(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	minimal := templ.ComponentFunc(func(_ context.Context, writer io.Writer) error { _, err := io.WriteString(writer, "safe"); return err })
	if _, err := renderConsole(cancelled, minimal); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled render error=%v", err)
	}
	oversized := templ.ComponentFunc(func(_ context.Context, writer io.Writer) error {
		_, err := writer.Write([]byte(strings.Repeat("x", maxConsolePatchBytes+1)))
		return err
	})
	if _, err := renderConsole(context.Background(), oversized); err == nil || !strings.Contains(err.Error(), "bounded") {
		t.Fatalf("oversized render error=%v", err)
	}
}

func TestConsolePatchUsesOneRequestScopedDatastarEvent(t *testing.T) {
	request := httptest.NewRequest("GET", "http://console.test/console/fragments/surface", nil)
	recorder := httptest.NewRecorder()
	component := templ.ComponentFunc(func(_ context.Context, writer io.Writer) error {
		_, err := io.WriteString(writer, `<main id="workspace">escaped</main>`)
		return err
	})
	if err := patchConsole(recorder, request, component); err != nil {
		t.Fatal(err)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("content type=%q", contentType)
	}
	body := recorder.Body.String()
	if strings.Count(body, "event: datastar-patch-elements") != 1 || !strings.Contains(body, "data: elements <main id=\"workspace\">escaped</main>") {
		t.Fatalf("unexpected bounded SSE framing: %q", body)
	}
}
