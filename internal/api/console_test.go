package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/credentials"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/principalauth"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestConsoleFormDecoderAcceptsOneExactBoundedField(t *testing.T) {
	valid := httptest.NewRequest("POST", "/console/login", strings.NewReader("password=bounded%2Bpassword"))
	valid.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	value, err := decodeConsoleForm(valid, "password")
	if err != nil || value != "bounded+password" {
		t.Fatalf("valid native form value=%q err=%v", value, err)
	}

	for name, request := range map[string]*http.Request{
		"wrong content type": httptest.NewRequest("POST", "/console/login", strings.NewReader("password=value")),
		"unknown field":      httptest.NewRequest("POST", "/console/login", strings.NewReader("password=value&authority=admin")),
		"duplicate field":    httptest.NewRequest("POST", "/console/login", strings.NewReader("password=one&password=two")),
		"oversized":          httptest.NewRequest("POST", "/console/login", bytes.NewReader(bytes.Repeat([]byte("x"), 8193))),
	} {
		t.Run(name, func(t *testing.T) {
			if name != "wrong content type" {
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			if _, err := decodeConsoleForm(request, "password"); err == nil {
				t.Fatal("unsafe native form accepted")
			}
		})
	}
}

func TestAgentOperationFormDecoderAcceptsOnlyExactBoundedArtifacts(t *testing.T) {
	validValues := url.Values{
		"csrf":      {"csrf-token"},
		"charter":   {`{"agent_id":"agent-alpha"}`},
		"fixture":   {`{"fleet_id":"fleet-primary"}`},
		"fleet_id":  {"fleet-primary"},
		"source_id": {"fleet-agent-1"},
	}
	valid := httptest.NewRequest(http.MethodPost, "/console/agents/registration/review", strings.NewReader(validValues.Encode()))
	valid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form, err := decodeAgentOperationForm(valid)
	if err != nil || form.CSRF != "csrf-token" || form.FleetID != "fleet-primary" || form.SourceID != "fleet-agent-1" {
		t.Fatalf("valid Agent operation form=%+v err=%v", form, err)
	}

	for name, mutate := range map[string]func(url.Values){
		"unknown authority field": func(values url.Values) { values.Set("principal", "forged") },
		"duplicate source":        func(values url.Values) { values["source_id"] = []string{"fleet-agent-1", "fleet-agent-2"} },
		"missing charter":         func(values url.Values) { values.Del("charter") },
		"oversized charter":       func(values url.Values) { values.Set("charter", strings.Repeat("x", 131073)) },
		"oversized source id":     func(values url.Values) { values.Set("source_id", strings.Repeat("x", 64)) },
	} {
		t.Run(name, func(t *testing.T) {
			values := url.Values{}
			for key, items := range validValues {
				values[key] = append([]string(nil), items...)
			}
			mutate(values)
			request := httptest.NewRequest(http.MethodPost, "/console/agents/registration/review", strings.NewReader(values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if _, err := decodeAgentOperationForm(request); err == nil {
				t.Fatal("unsafe Agent operation form accepted")
			}
		})
	}
}

func TestConsoleOperationFormDecoderAcceptsOnlyCSRFAndClosedOperation(t *testing.T) {
	valid := httptest.NewRequest(http.MethodPost, "/console/queue/item/operate", strings.NewReader("csrf=session-token&operation=cancel"))
	valid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	csrf, operation, err := decodeConsoleOperationForm(valid)
	if err != nil || csrf != "session-token" || operation != "cancel" {
		t.Fatalf("valid operation form csrf=%q operation=%q err=%v", csrf, operation, err)
	}

	for name, body := range map[string]string{
		"missing csrf":        "operation=cancel",
		"missing operation":   "csrf=session-token",
		"unknown field":       "csrf=session-token&operation=cancel&authority=admin",
		"duplicate csrf":      "csrf=one&csrf=two&operation=cancel",
		"duplicate operation": "csrf=session-token&operation=cancel&operation=revoke",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/console/queue/item/operate", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if _, _, err := decodeConsoleOperationForm(request); err == nil {
				t.Fatal("unsafe queue operation form accepted")
			}
		})
	}
}

func TestAgentExecuteFormDecoderAcceptsOnlyCSRFAndStrictReceipt(t *testing.T) {
	valid := url.Values{"csrf": {"csrf-token"}, "receipt": {strings.Repeat("a", 64)}}
	request := httptest.NewRequest(http.MethodPost, "/console/agents/registration/execute", strings.NewReader(valid.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form, err := decodeAgentExecuteForm(request)
	if err != nil || form.CSRF != "csrf-token" || form.Receipt != strings.Repeat("a", 64) {
		t.Fatalf("valid execute form=%+v err=%v", form, err)
	}
	for name, mutate := range map[string]func(url.Values){
		"raw charter substitution": func(values url.Values) { values.Set("charter", `{}`) },
		"raw fixture substitution": func(values url.Values) { values.Set("fixture", `{}`) },
		"fleet substitution":       func(values url.Values) { values.Set("fleet_id", "other") },
		"duplicate receipt": func(values url.Values) {
			values["receipt"] = []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}
		},
		"uppercase receipt": func(values url.Values) { values.Set("receipt", strings.Repeat("A", 64)) },
		"short receipt":     func(values url.Values) { values.Set("receipt", "abc") },
	} {
		t.Run(name, func(t *testing.T) {
			values := url.Values{}
			for key, items := range valid {
				values[key] = append([]string(nil), items...)
			}
			mutate(values)
			r := httptest.NewRequest(http.MethodPost, "/console/agents/registration/execute", strings.NewReader(values.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if _, decodeErr := decodeAgentExecuteForm(r); decodeErr == nil {
				t.Fatal("unsafe execute form accepted")
			}
		})
	}
}

func TestAgentLifecycleFormDecoderRequiresExactRevisionAndRetirementConfirmation(t *testing.T) {
	valid := url.Values{"csrf": {"csrf-token"}, "revision": {"1"}, "digest": {"sha256:" + strings.Repeat("a", 64)}, "lifecycle": {"disabled"}}
	request := httptest.NewRequest(http.MethodPost, "/console/agents/agent-alpha/lifecycle", strings.NewReader(valid.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form, err := decodeAgentLifecycleForm(request)
	if err != nil || form.Lifecycle != "disabled" || form.Revision != "1" {
		t.Fatalf("valid lifecycle form=%+v err=%v", form, err)
	}

	for name, mutate := range map[string]func(url.Values){
		"unknown lifecycle":                 func(values url.Values) { values.Set("lifecycle", "active") },
		"retirement without confirmation":   func(values url.Values) { values.Set("lifecycle", "retired") },
		"confirmation on reversible action": func(values url.Values) { values.Set("confirm_retirement", "retire") },
		"duplicate retirement confirmation": func(values url.Values) {
			values.Set("lifecycle", "retired")
			values["confirm_retirement"] = []string{"retire", "retire"}
		},
		"forged authority": func(values url.Values) { values.Set("stanza", "admin") },
	} {
		t.Run(name, func(t *testing.T) {
			values := url.Values{}
			for key, items := range valid {
				values[key] = append([]string(nil), items...)
			}
			mutate(values)
			request := httptest.NewRequest(http.MethodPost, "/console/agents/agent-alpha/lifecycle", strings.NewReader(values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if _, err := decodeAgentLifecycleForm(request); err == nil {
				t.Fatal("unsafe lifecycle form accepted")
			}
		})
	}

	retirement := url.Values{}
	for key, items := range valid {
		retirement[key] = append([]string(nil), items...)
	}
	retirement.Set("lifecycle", "retired")
	retirement.Set("confirm_retirement", "retire")
	retireRequest := httptest.NewRequest(http.MethodPost, "/console/agents/agent-alpha/lifecycle", strings.NewReader(retirement.Encode()))
	retireRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if form, err = decodeAgentLifecycleForm(retireRequest); err != nil || form.Lifecycle != "retired" {
		t.Fatalf("confirmed retirement form=%+v err=%v", form, err)
	}
}
func TestPasswordRotationFormRequiresExactClosedFieldSet(t *testing.T) {
	valid := httptest.NewRequest(http.MethodPost, "/console/password", strings.NewReader("current_password=current-value&new_password=replacement-value&confirmation=replacement-value&csrf=csrf-value&approve=rotate"))
	valid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form, err := decodePasswordRotationForm(valid)
	if err != nil || form.Current != "current-value" || form.New != "replacement-value" || form.Confirmation != form.New || form.CSRF != "csrf-value" || !form.Approved {
		t.Fatalf("valid rotation form=%+v err=%v", form, err)
	}
	for name, raw := range map[string]string{
		"missing approval": "current_password=current-value&new_password=replacement-value&confirmation=replacement-value&csrf=csrf-value",
		"unknown field":    "current_password=current-value&new_password=replacement-value&confirmation=replacement-value&csrf=csrf-value&approve=rotate&authority=admin",
		"duplicate":        "current_password=current-value&current_password=other&new_password=replacement-value&confirmation=replacement-value&csrf=csrf-value&approve=rotate",
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/console/password", strings.NewReader(raw))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if _, err := decodePasswordRotationForm(request); err == nil {
				t.Fatal("unsafe rotation form accepted")
			}
		})
	}
}

func TestPrincipalVerifierReplacementRollsBackWhenAuditFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", principalauth.FileName)
	current, err := principalauth.Enroll("principal", []byte("current-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := principalauth.Enroll("principal", []byte("replacement-principal-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err = principalauth.Publish(path, current); err != nil {
		t.Fatal(err)
	}
	if err = replacePrincipalVerifier(path, current, replacement, func() error { return nil }, func() error { return errors.New("audit unavailable") }); err == nil {
		t.Fatal("audit failure was ignored")
	}
	loaded, err := principalauth.Load(path)
	if err != nil || loaded != current {
		t.Fatalf("audit failure did not restore current verifier: loaded=%+v err=%v", loaded, err)
	}
	if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("rollback mode=%v err=%v", info.Mode().Perm(), statErr)
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
		Credentials: []app.CredentialView{{ID: "secret-1", Reference: "github", Kind: "environment", Status: "active", CurrentVersion: 2}},
		Readiness: map[string]app.SurfaceReadiness{
			"credentials": {State: "ready", ReasonCode: "credentials_authority_read_succeeded", Source: "credentials.authority.bbolt", Count: 1, Authoritative: true},
		},
	}, consoleCredentials)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.Title != "Credentials" || credentials.State != "ready" || len(credentials.Records) != 1 {
		t.Fatalf("credential surface=%+v", credentials)
	}
	label := credentials.Records[0].Label
	if label != "github · active · v2" {
		t.Fatalf("credential record label=%q", label)
	}
	if strings.Contains(credentials.Records[0].JSON, "source_env") || strings.Contains(credentials.Records[0].JSON, "target_env") {
		t.Fatalf("credential surface exposed custody details: %s", credentials.Records[0].JSON)
	}
	if strings.Contains(credentials.Records[0].JSON, "Ciphertext") || strings.Contains(credentials.Records[0].JSON, "WrappedDEK") || strings.Contains(credentials.Records[0].JSON, "RecordNonce") {
		t.Fatalf("credential surface exposed encrypted payload: %s", credentials.Records[0].JSON)
	}
}

func TestLoopConsoleRecordShowsValidationLifecycleAndAuthorityProvenance(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	revision, validation, err := loop.NewRevision(loop.LoopRevision{
		LoopID: "loop.review", Revision: 3, PreviousDigest: digest, EntryStepID: "review",
		Steps: []loop.Step{
			{ID: "review", Kind: loop.StepAction, Retry: loop.RetryPolicy{MaxAttempts: 2}, EvidenceClaims: []loop.EvidenceClaim{{Claim: "review-receipt", MediaType: "application/json", ExpectedDigest: digest, VerifierID: evidence.ArtifactVerifierID, PolicyVersion: evidence.VerifierPolicyV1}}},
			{ID: "done", Kind: loop.StepTerminal, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}},
		},
		Transitions:      []loop.Transition{{ID: "complete", FromStepID: "review", ToStepID: "done"}},
		RequiredEvidence: []loop.EvidenceRequirement{{Claim: "review-receipt", ProducerStepID: "review"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	loopRef := loop.NewProvenanceRevision(revision.LoopID, revision.Revision, revision.Digest)
	publisher := loop.NewProvenanceRevision("agent-reviewer", 7, digest)
	authority := loop.NewProvenanceDigest("authority-review", digest)
	provenance, err := loop.NewPublicationProvenance(loop.PublicationProvenance{
		Loop: loopRef, PublisherAgent: publisher, Authority: authority, MandateID: "mandate-review", StanzaID: "operator",
		Runtime: loop.ProvenanceRuntime{Runtime: "hermes-agent"}, Charter: publisher, ValidationDigest: validation.Digest,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := loop.NewLifecycleEvent(loop.LifecycleEvent{
		EventID: "activate-review", LoopID: revision.LoopID, State: loop.LifecycleActive, Revision: loopRef,
		PublisherAgent: publisher, Authority: authority, MandateID: "mandate-review", StanzaID: "operator", OccurredAt: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	view := app.LoopView{Revision: revision, Validations: []loop.LoopValidationResult{validation}, Provenance: provenance, Lifecycle: loop.Lifecycle{LoopID: revision.LoopID, State: loop.LifecycleActive, ActiveRevision: revision.Revision, ActiveDigest: revision.Digest}, History: []loop.LifecycleEvent{event}}
	record := consoleLoopRecord(view)
	if record.Key != "loop.review:3" || record.Lifecycle != "active" || record.Runtime != "hermes-agent" || record.Source != "agent-reviewer" || record.Authority != "authority-review" {
		t.Fatalf("Loop summary lost exact bindings: %+v", record)
	}
	values := make(map[string]string, len(record.Fields))
	for _, field := range record.Fields {
		values[field.Label] = field.Value
	}
	for label, fragment := range map[string]string{
		"Executable steps": "review · action · max 2 attempt(s)", "Transitions": "review → done",
		"Evidence contract": "review-receipt", "Validation": string(loop.ValidationValid),
		"Lifecycle history": "1 immutable event(s)", "Publisher Agent": "agent-reviewer revision 7",
		"Authority provenance": "mandate mandate-review · stanza operator", "Immutable revision": revision.Digest,
	} {
		if !strings.Contains(values[label], fragment) {
			t.Fatalf("Loop field %q=%q, want fragment %q", label, values[label], fragment)
		}
	}
	view.Lifecycle.ActiveRevision = revision.Revision - 1
	view.Lifecycle.ActiveDigest = digest
	historical := consoleLoopRecord(view)
	activeLabel := exactRevisionLabel(revision.LoopID, revision.Revision-1, digest)
	if historical.Lifecycle != "inactive" || strings.Contains(historical.Readiness, "Active exact revision") || !strings.Contains(historical.Readiness, activeLabel) {
		t.Fatalf("historical Loop revision was mislabeled: %+v", historical)
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
	view.Lifecycle.ActiveRevision = revision.Revision - 1
	view.Lifecycle.ActiveDigest = digest("9")
	historical := consoleGraphRecord(view, history, `{"graph_id":"graph-review"}`)
	activeLabel := exactRevisionLabel(revision.GraphID, revision.Revision-1, digest("9"))
	if historical.Lifecycle != "inactive" || strings.Contains(historical.Readiness, "Active exact revision") || !strings.Contains(historical.Readiness, activeLabel) {
		t.Fatalf("historical Graph revision was mislabeled: %+v", historical)
	}
}

func TestConsoleQueueRecordPreservesAuthoritativeFailureAndExactProvenance(t *testing.T) {
	digest := func(marker string) string { return "sha256:" + strings.Repeat(marker, 64) }
	at := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	view := app.QueueExecutionView{
		Item: queue.Item{ItemID: "queue-130", Digest: digest("1"), State: queue.StateQueued, MaxAttempts: 3, EnqueuedAt: at,
			Submission: reference.DigestRef{ID: "submission-130", Digest: digest("2")}, Snapshot: reference.DigestRef{ID: "snapshot-130", Digest: digest("3")}, Authority: reference.DigestRef{ID: "authority-130", Digest: digest("4")}},
		Projection: queue.Projection{QueueItemID: "queue-130", State: queue.StateFailed, Attempts: 1, AvailableAt: at},
		GraphRun:   execution.GraphRun{GraphRunID: "graph-run-130", State: execution.StateSucceeded, Snapshot: reference.DigestRef{ID: "snapshot-130", Digest: digest("3")}, Digest: digest("5")},
		LoopExecutions: []execution.LoopExecution{{LoopExecutionID: "loop-exec-130", GraphNodeID: "review", State: execution.StateFailed, CreatedAt: at,
			Loop: reference.RevisionRef{ID: "loop-review", Revision: 7, Digest: digest("6")}, Participant: reference.RevisionRef{ID: "agent-reviewer", Revision: 2, Digest: digest("7")}, Digest: digest("8")}},
		Attempts:    []execution.Attempt{{AttemptID: "attempt-130", LoopExecutionID: "loop-exec-130", ClaimID: "claim-130", AttemptNumber: 1, State: execution.StateFailed, CreatedAt: at, Digest: digest("9")}},
		Runtime:     registry.RuntimeBinding{Adapter: "hermes", Runtime: "hermes-agent", Target: "aegis-owned-ephemeral"},
		Artifact:    &evidence.RuntimeArtifact{ID: "artifact-130", AttemptID: "attempt-130", ActionID: "review", RunID: "graph-run-130", Digest: digest("a"), ContentRef: digest("a"), MediaType: "application/json", CreatedAt: at},
		Receipts:    []evidence.VerificationReceipt{{ID: "receipt-130", Outcome: evidence.Passed, Claim: "review-receipt", VerifierID: "artifact-verifier", PolicyVersion: "v1", ExpectedDigest: digest("a"), ObservedDigest: digest("a"), ObservedAt: at}},
		Disposition: &disposition.Record{DispositionID: "disposition-130", Digest: digest("b"), State: execution.StateFailed, ReasonCode: "runtime_exit_nonzero", AttemptID: "attempt-130", OccurredAt: at},
	}
	record := consoleQueueRecord(view)
	if record.Key != "queue-130" || record.Lifecycle != "failed" || record.Queue == nil || record.Queue.GraphRun.State != "succeeded" {
		t.Fatalf("queue truth was collapsed or upgraded: %+v", record)
	}
	wantBinding := exactRevisionLabel("loop-review", 7, digest("6")) + " · participant " + exactRevisionLabel("agent-reviewer", 2, digest("7"))
	if len(record.Queue.Loops) != 1 || record.Queue.Loops[0].Binding != wantBinding {
		t.Fatalf("exact Loop/participant provenance lost: %+v", record.Queue.Loops)
	}
	if len(record.Queue.Attempts) != 1 || record.Queue.Attempts[0].ClaimID != "claim-130" || len(record.Queue.Receipts) != 1 || record.Queue.Receipts[0].Outcome != "passed" {
		t.Fatalf("attempt or receipt provenance lost: %+v", record.Queue)
	}
	if record.Queue.Disposition[1].Value != "failed" || record.Queue.Disposition[2].Value != "runtime_exit_nonzero" {
		t.Fatalf("authoritative disposition lost: %+v", record.Queue.Disposition)
	}
}

func TestConsoleQueueFilterSeparatesLifecycleViews(t *testing.T) {
	model := consoleweb.SurfaceModel{
		State: "ready",
		Records: []consoleweb.RecordModel{
			{Key: "queued", Lifecycle: "queued"},
			{Key: "failed", Lifecycle: "failed"},
		},
	}
	if err := filterConsoleQueue(&model, "failed"); err != nil {
		t.Fatal(err)
	}
	if model.QueueState != "failed" || len(model.Records) != 1 || model.Records[0].Key != "failed" {
		t.Fatalf("filtered queue model=%+v", model)
	}
	if err := filterConsoleQueue(&model, "unknown"); err == nil {
		t.Fatal("unknown queue state filter accepted")
	}

	empty := consoleweb.SurfaceModel{State: "ready", Records: []consoleweb.RecordModel{{Key: "queued", Lifecycle: "queued"}}}
	if err := filterConsoleQueue(&empty, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if empty.State != "filtered-empty" || len(empty.Records) != 0 {
		t.Fatalf("empty filtered queue model=%+v", empty)
	}
}

func TestConsoleQueueRecordSeparatesLifecycleViewsAndOrdersCausalHistory(t *testing.T) {
	now := time.Now().UTC()
	base := app.QueueExecutionView{
		Item:       queue.Item{ItemID: "queue-lifecycle", MaxAttempts: 2, EnqueuedAt: now.Add(-time.Hour)},
		Projection: queue.Projection{State: queue.StateQueued, AvailableAt: now.Add(time.Hour)},
		GraphRun:   execution.GraphRun{GraphRunID: "run-lifecycle"},
	}
	terminal := func(state execution.State, reason string) *disposition.Record {
		return &disposition.Record{DispositionID: "disposition-lifecycle", State: state, ReasonCode: reason, OccurredAt: now}
	}
	cases := []struct {
		name string
		view app.QueueExecutionView
		want string
	}{
		{"queued", base, "queued"},
		{"claimable", func() app.QueueExecutionView { v := base; v.Projection.AvailableAt = now.Add(-time.Minute); return v }(), "claimable"},
		{"active", func() app.QueueExecutionView { v := base; v.Projection.State = queue.StateClaimed; return v }(), "active"},
		{"retrying", func() app.QueueExecutionView { v := base; v.Retries = []queue.Retry{{RetryID: "retry-1"}}; return v }(), "retrying"},
		{"cancelled", func() app.QueueExecutionView { v := base; v.Projection.State = queue.StateCancelled; return v }(), "cancelled"},
		{"expired", func() app.QueueExecutionView { v := base; v.Projection.State = queue.StateExpired; return v }(), "expired"},
		{"denied", func() app.QueueExecutionView {
			v := base
			v.Projection.State = queue.StateDenied
			v.Disposition = terminal(execution.StateDenied, "authority_revoked")
			return v
		}(), "denied"},
		{"failed", func() app.QueueExecutionView {
			v := base
			v.Projection.State = queue.StateFailed
			v.Disposition = terminal(execution.StateFailed, "runtime_failed")
			return v
		}(), "failed"},
		{"exhausted", func() app.QueueExecutionView {
			v := base
			v.Projection.State = queue.StateFailed
			v.Disposition = terminal(execution.StateFailed, "retry_exhausted")
			return v
		}(), "exhausted"},
		{"succeeded", func() app.QueueExecutionView {
			v := base
			v.Projection.State = queue.StateSucceeded
			v.Disposition = terminal(execution.StateSucceeded, "verified_success")
			return v
		}(), "succeeded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := consoleQueueRecord(tc.view).Lifecycle; got != tc.want {
				t.Fatalf("lifecycle=%q, want %q", got, tc.want)
			}
		})
	}
	view := base
	view.Projection.State, view.Projection.Attempts = queue.StateClaimed, 1
	view.Claims = []queue.Claim{{ClaimID: "claim-1", WorkerID: "worker-1", ClaimedAt: now.Add(-50 * time.Minute), ExpiresAt: now.Add(-40 * time.Minute)}}
	view.Attempts = []execution.Attempt{{AttemptID: "attempt-1", ClaimID: "claim-1", AttemptNumber: 1, CreatedAt: now.Add(-49 * time.Minute)}}
	view.Transitions = []queue.QueueTransition{{TransitionID: "transition-1", From: queue.StateQueued, To: queue.StateClaimed, OccurredAt: now.Add(-50 * time.Minute)}}
	view.Retries = []queue.Retry{{RetryID: "retry-1", AttemptNumber: 1, Reclaimed: true, OccurredAt: now.Add(-39 * time.Minute), AvailableAt: now.Add(-38 * time.Minute)}}
	record := consoleQueueRecord(view)
	if len(record.Queue.Timeline) < 5 {
		t.Fatalf("incomplete causal timeline: %+v", record.Queue.Timeline)
	}
	for index := 1; index < len(record.Queue.Timeline); index++ {
		if record.Queue.Timeline[index].At < record.Queue.Timeline[index-1].At {
			t.Fatalf("timeline is not causal: %+v", record.Queue.Timeline)
		}
	}
	if record.Queue.Controls[0].Enabled || !record.Queue.Controls[1].Enabled || !record.Queue.Controls[3].Enabled || record.Queue.Controls[4].Enabled {
		t.Fatalf("control eligibility is not fail-closed: %+v", record.Queue.Controls)
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

func TestConsoleCredentialRecordPreservesActiveRevokedAndVaultMetadata(t *testing.T) {
	at := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	active := app.CredentialView{
		ID: "secret-active", Reference: "github/api", Kind: "api-token", Status: "active",
		CurrentVersion: 2, CreatedAt: at.Format(time.RFC3339), CreatedBy: "principal-1",
		BindingCount: 3,
		VersionHistory: []app.CredentialVersionView{
			{Version: 1, FormatVersion: 1, Algorithm: "xchacha20-poly1305", KEKVersion: 1, CreatedAt: at, CiphertextHash: "sha256:" + strings.Repeat("a", 64)},
			{Version: 2, FormatVersion: 1, Algorithm: "xchacha20-poly1305", KEKVersion: 1, CreatedAt: at.Add(time.Hour), CiphertextHash: "sha256:" + strings.Repeat("b", 64)},
		},
	}
	revoked := app.CredentialView{
		ID: "secret-revoked", Reference: "github/legacy", Kind: "api-token", Status: "revoked",
		CurrentVersion: 1, CreatedAt: at.Format(time.RFC3339), CreatedBy: "principal-1",
		RevokedAt: at.Add(2 * time.Hour).Format(time.RFC3339), Revocation: "rotation",
		BindingCount: 0,
	}
	vault := app.VaultStatusView{State: "initialized", ReasonCode: "credentials_vault_ready"}
	vault.VaultStatus = credentialsVaultForTest()
	activeRecord := consoleCredentialRecord(active, vault)
	if activeRecord.Lifecycle != "active" || activeRecord.Credential == nil || activeRecord.Credential.Reference != "github/api" {
		t.Fatalf("active credential record lost: %+v", activeRecord)
	}
	if len(activeRecord.Credential.Versions) != 2 {
		t.Fatalf("encrypted version history was not projected: %+v", activeRecord.Credential.Versions)
	}
	if activeRecord.Credential.Vault.KEKID == "" || activeRecord.Credential.Vault.KEKVersion == 0 {
		t.Fatalf("KEK metadata missing: %+v", activeRecord.Credential.Vault)
	}
	for _, label := range []string{"Stable record ID", "Bindings", "Version history"} {
		found := false
		for _, f := range activeRecord.Fields {
			if f.Label == label {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("active credential missing field %q: %+v", label, activeRecord.Fields)
		}
	}
	revokedRecord := consoleCredentialRecord(revoked, vault)
	if revokedRecord.Lifecycle != "revoked" || revokedRecord.Credential == nil {
		t.Fatalf("revoked credential record lost: %+v", revokedRecord)
	}
	if revokedRecord.Credential.Revocation != "rotation" {
		t.Fatalf("revocation reason lost: %+v", revokedRecord.Credential)
	}
	hasRevokedAt := false
	for _, f := range revokedRecord.Fields {
		if f.Label == "Revoked at" {
			hasRevokedAt = true
		}
	}
	if !hasRevokedAt {
		t.Fatalf("revoked credential missing revoked-at field: %+v", revokedRecord.Fields)
	}
	for _, json := range []string{activeRecord.JSON, revokedRecord.JSON} {
		for _, forbidden := range []string{"source_env", "target_env", "AEGIS_API_TEST_KEY", `Ciphertext":`, `WrappedDEK":`, `RecordNonce":`, `WrapNonce":`, `KEKID":`, `ciphertext":`, `wrapped_dek":`, `record_nonce":`, `wrap_nonce":`} {
			if strings.Contains(json, forbidden) {
				t.Fatalf("credential JSON exposed %s: %s", forbidden, json)
			}
		}
	}
	if !strings.Contains(activeRecord.JSON, "\"status\": \"active\"") || !strings.Contains(revokedRecord.JSON, "\"status\": \"revoked\"") {
		t.Fatalf("active/revoked status was not projected into JSON: active=%s revoked=%s", activeRecord.JSON, revokedRecord.JSON)
	}
}

func TestConsoleCredentialFilterSeparatesLifecycleAndSearches(t *testing.T) {
	model := consoleweb.SurfaceModel{
		Domain: string(consoleCredentials), State: "ready", Authoritative: true, TotalCount: 3,
		Records: []consoleweb.RecordModel{
			{Key: "secret-1", Label: "github/api", Lifecycle: "active"},
			{Key: "secret-2", Label: "github/legacy", Lifecycle: "revoked"},
			{Key: "secret-3", Label: "openai/api", Lifecycle: "active"},
		},
	}
	if err := filterConsoleCredentials(&model, "github", "all"); err != nil {
		t.Fatal(err)
	}
	if len(model.Records) != 2 {
		t.Fatalf("expected 2 github records, got %d", len(model.Records))
	}
	if err := filterConsoleCredentials(&model, "", "revoked"); err != nil {
		t.Fatal(err)
	}
	if len(model.Records) != 1 || model.Records[0].Label != "github/legacy" {
		t.Fatalf("revoked filter did not isolate: %+v", model.Records)
	}
	if model.Lifecycle != "revoked" || model.Query != "" {
		t.Fatalf("filter state not echoed: %+v", model)
	}
	for _, input := range []struct{ query, status string }{
		{strings.Repeat("x", 129), "all"}, {"x\ny", "all"}, {"", "expired"},
	} {
		if err := filterConsoleCredentials(&model, input.query, input.status); err == nil {
			t.Fatalf("unsafe credential filter accepted: %+v", input)
		}
	}
}

func credentialsVaultForTest() credentials.VaultStatus {
	return credentials.VaultStatus{
		Database: "/state/credentials/authority.db", DeploymentID: "deployment-test", StoreID: "store-test",
		KEKID: "kek-1", KEKVersion: 1, SchemaVersion: "1", Custody: "host-file", LastCleanShutdown: true,
	}
}
