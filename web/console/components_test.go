package consoleweb

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRecordLabelEscapesAdversarialFleetText(t *testing.T) {
	var output bytes.Buffer
	value := `</span><script>globalThis.pwned=1</script>`
	if err := RecordLabel(value).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "<script>") || !strings.Contains(output.String(), "&lt;script") {
		t.Fatalf("untrusted record was not escaped: %s", output.String())
	}
}

func TestDocumentUsesNativeInteractionsUnderStrictCSP(t *testing.T) {
	var output bytes.Buffer
	if err := Document(PageModel{}).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if strings.Contains(html, "Authenticated control plane") || !strings.Contains(html, "Authentication required") {
		t.Fatalf("signed-out header asserted authenticated state: %s", html)
	}
	for _, required := range []string{"<nav", "<main", "aria-live", `id="authentication-status"`, `data-state="loading"`, `data-state="empty"`, `data-state="denied"`, `data-state="unavailable"`, `data-state="degraded_repair_required"`, `data-state="error"`, `method="post"`, `action="/console/login"`} {
		if !strings.Contains(html, required) {
			t.Fatalf("document missing %q", required)
		}
	}
	for _, forbidden := range []string{"<script", "data-on:", "data-bind:", "localStorage", "sessionStorage"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("document contains CSP-incompatible browser behavior %q", forbidden)
		}
	}
}

func TestAuthenticatedDocumentUsesNativeNavigationInspectionAndLogout(t *testing.T) {
	var output bytes.Buffer
	model := PageModel{
		Authenticated: true,
		CSRF:          "csrf-value",
		Surface: SurfaceModel{
			Domain:        DomainAgents,
			Authoritative: true,
			TotalCount:    1,
			Records:       []RecordModel{{Key: "0", Label: "Agent one"}},
			Inspector:     &RecordModel{Key: "0", Label: "Agent one"},
			InspectorOpen: true,
		},
	}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{
		`action="/console/logout"`, `name="csrf"`, `value="csrf-value"`,
		`href="/console/agents#/agents"`, `href="/console/graphs#/graphs"`,
		`href="/console/loops#/loops"`, `href="/console/queue#/queue"`,
		`href="/console/credentials#/credentials"`, `record_key=0`, `id="close-inspector"`,
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("native interaction missing %q: %s", required, html)
		}
	}
	if strings.Contains(html, "data-on:") || strings.Contains(html, "data-signals") {
		t.Fatal("authenticated document still depends on runtime expression evaluation")
	}
}

func TestAuthenticatedDocumentPresentsPasswordRotationAsDangerousDialogAction(t *testing.T) {
	var output bytes.Buffer
	model := PageModel{
		Authenticated: true,
		CSRF:          "csrf-value",
		Surface:       SurfaceModel{Domain: DomainAgents, Title: "Agent Registry"},
	}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{
		`id="open-password-rotation" type="button" class="ghost btn-sm" commandfor="principal-password-rotation" command="show-modal"`,
		`<dialog id="principal-password-rotation"`, `data-overlay-kind="dialog"`,
		`action="/console/password"`, `name="csrf" value="csrf-value"`,
		`name="current_password" type="password" value="" required autocomplete="current-password"`,
		`name="new_password" type="password" value="" required autocomplete="new-password" minlength="12"`,
		`name="confirmation" type="password" value="" required autocomplete="new-password" minlength="12"`,
		`name="approve" type="checkbox" value="rotate" required`,
		`class="confirmation danger"`, `class="danger-button"`,
		"revokes every existing browser session and one-time handoff",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("password rotation dialog missing %q: %s", required, html)
		}
	}
	if strings.Contains(html, `<details class="panel" id="principal-password-rotation"`) {
		t.Fatalf("password rotation still renders as a permanently appended details form: %s", html)
	}
	for _, name := range []string{"current_password", "new_password", "confirmation", "approve"} {
		if strings.Count(html, `name="`+name+`"`) != 1 {
			t.Fatalf("password rotation field %q must render exactly once: %s", name, html)
		}
	}
	if strings.Count(html, `name="csrf" value="csrf-value"`) != 2 {
		t.Fatalf("logout and password rotation must each carry the authenticated CSRF value: %s", html)
	}
}

func TestWorkspaceEscapesContextualReadinessAndDisablesDeniedActions(t *testing.T) {
	var output bytes.Buffer
	hostile := `</span><script>globalThis.pwned=1</script>`
	model := PageModel{
		Authenticated: true,
		Surface: SurfaceModel{
			Domain:      DomainAgents,
			Title:       hostile,
			Eyebrow:     hostile,
			Description: hostile,
			State:       "denied",
			Status:      hostile,
			ReasonCode:  hostile,
			Actions: []ActionModel{{
				Key:           "register_fleet_agent",
				Label:         hostile,
				State:         "denied",
				ReasonCode:    hostile,
				RepairActions: []string{hostile},
				Primary:       true,
			}},
		},
	}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if strings.Contains(html, "<script>") || strings.Count(html, "&lt;script") < 7 {
		t.Fatalf("contextual workspace values were not escaped: %s", html)
	}
	if !strings.Contains(html, `<button class="primary" type="button" disabled`) || !strings.Contains(html, `data-state="denied"`) {
		t.Fatalf("denied action was not visibly fail-closed: %s", html)
	}
	if !strings.Contains(html, "Count unavailable") || strings.Contains(html, "0 agents") {
		t.Fatalf("non-authoritative collection asserted a fabricated count: %s", html)
	}
}

func TestReadyAgentActionLinksToDedicatedCharterImportPage(t *testing.T) {
	var output bytes.Buffer
	model := PageModel{Authenticated: true, Surface: SurfaceModel{
		Domain: DomainAgents, Title: "Agent Registry", State: "ready", Authoritative: true,
		Actions: []ActionModel{{Key: "register_fleet_agent", Label: "Prepare charter import", State: "ready", ReasonCode: "ready", Primary: true}},
	}}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{`href="/console/agents/charter-import"`, "Prepare charter import"} {
		if !strings.Contains(html, required) {
			t.Fatalf("ready Agent charter-import link missing %q: %s", required, html)
		}
	}
	for _, absent := range []string{`href="#charter-import-review"`, `id="charter-import-review"`, "aegis charter validate", "aegis charter import"} {
		if strings.Contains(html, absent) {
			t.Fatalf("Agent Registry retained embedded charter-import content %q: %s", absent, html)
		}
	}
}

func TestCharterImportPageRendersDistinctReviewOnlyGuidance(t *testing.T) {
	var output bytes.Buffer
	model := PageModel{Authenticated: true, CharterImport: true, Surface: SurfaceModel{
		Domain: DomainAgents,
		CharterImportProposal: CharterImportProposal{
			Notice:          "Review only; the browser cannot import a charter or grant authority.",
			ValidateCommand: "aegis charter validate <charter-file.json>",
			ImportCommand:   "aegis charter import <charter-file.json>",
		},
	}}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{"<title>Charter import review · Aegis Console</title>", `id="charter-import-title"`, "Charter import review", "Agent Registry", `href="/console/agents#/agents"`, "Back to Agent Registry", "Review only", "aegis charter validate &lt;charter-file.json&gt;", "aegis charter import &lt;charter-file.json&gt;"} {
		if !strings.Contains(html, required) {
			t.Fatalf("charter import page missing %q: %s", required, html)
		}
	}
	if strings.Contains(html, "data-on:") || strings.Contains(html, `action="/console/agents/charter-import"`) {
		t.Fatalf("charter import page gained charter-import mutation behavior: %s", html)
	}
}

func TestAuthenticationRendersTypedRecoveryWithoutSubmittedBootstrap(t *testing.T) {
	var output bytes.Buffer
	model := AuthenticationModel{Status: "Authentication failed", ReasonCode: "bootstrap_invalid_format", RecoveryCommand: "aegis console", BootstrapTTL: "30s", SessionTTL: "5m0s"}
	if err := Authentication(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{"Authentication failed", "bootstrap_invalid_format", "aegis console", "30s", "5m0s"} {
		if !strings.Contains(html, required) {
			t.Fatalf("authentication recovery missing %q: %s", required, html)
		}
	}
	if strings.Contains(html, `value=`) {
		t.Fatalf("authentication error re-rendered a submitted value: %s", html)
	}
}

func TestAuthoritativeCollectionRendersAuthoritativeTotal(t *testing.T) {
	var output bytes.Buffer
	model := PageModel{Authenticated: true, Surface: SurfaceModel{
		Domain: DomainAgents, Title: "Agent Registry", State: "ready", Authoritative: true, TotalCount: 2,
		Records: []RecordModel{{Key: "0", Label: "Agent one"}},
	}}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if !strings.Contains(html, "2 agents") {
		t.Fatalf("workspace did not preserve the authoritative total: %s", html)
	}
}

func TestAgentRegistryRendersOperatorContractWithoutClaimingBrowserAuthority(t *testing.T) {
	var output bytes.Buffer
	record := RecordModel{
		Key: "office", Label: "office", Revision: "r3", Runtime: "hermes-local", Lifecycle: "enabled",
		Readiness: "Lifecycle eligible; fresh authority admission required", Source: "fleet-a / hermes-profile / office", Owner: "principal-1",
		Authority: "2 capabilities · 1 policy declared", Provisioning: "Not asserted by Registry record",
		Fields: []FieldModel{{Label: "Effective authority", Value: "Not evaluated by this Registry read"}},
	}
	model := PageModel{Authenticated: true, CSRF: "csrf", Surface: SurfaceModel{
		Domain: DomainAgents, Title: "Agent Registry", Eyebrow: "Participants", Description: "Existing fleet agents backed by immutable revisions.",
		State: "ready", Lifecycle: "enabled", Query: "office", Authoritative: true, TotalCount: 2, TotalRecords: 2,
		Actions: []ActionModel{{Key: "register_fleet_agent", Label: "Prepare charter import", State: "denied", ReasonCode: "principal_not_authorized", Primary: true}},
		Records: []RecordModel{record}, Inspector: &record, InspectorOpen: true,
	}}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{
		"Participants", "Prepare charter import", `disabled`, `name="q"`, `value="office"`, `name="lifecycle"`,
		"All lifecycle states", "Execution readiness", "Authority", "Provisioning", "Back to Registry",
		"Readiness is derived from the immutable lifecycle record", "browser view grants no runtime authority",
		"Not evaluated by this Registry read", `record_key=office`, `@media(max-width:700px)`,
	} {
		if !strings.Contains(html+string(CSS), required) {
			t.Fatalf("Agent Registry operator contract missing %q", required)
		}
	}
}

func TestGraphInspectorRendersTopologyExactBindingsAndFleetWideSubmissionTruth(t *testing.T) {
	var output bytes.Buffer
	record := RecordModel{Key: "graph-review:4", Label: "graph-review", Revision: "r4", Lifecycle: "active", Graph: &GraphDetailModel{
		Digest: "sha256:graph", PreviousDigest: "sha256:previous", Validation: "valid · sha256:validation",
		InputSchema: []FieldModel{{Label: "brief", Value: "string · required true"}}, OutputSchema: []FieldModel{{Label: "artifact", Value: "artifact · required true"}},
		Nodes:               []GraphNodeModel{{ID: "review", Participant: "agent-reviewer r2 @ sha256:agent", Loop: "loop-review r7 @ sha256:loop", Inputs: "brief:string required=true", Outputs: "draft:string required=true"}},
		Edges:               []GraphEdgeModel{{ID: "review-before-publish", From: "review", To: "publish", Mappings: "draft → draft"}},
		Policies:            []FieldModel{{Label: "operator", Value: "policy-operator @ sha256:policy"}},
		AcceptedRuns:        []GraphRunModel{{Submission: "submission-1 @ sha256:submission", Snapshot: "snapshot-1 @ sha256:snapshot", QueueItem: "item-1 @ sha256:item", GraphRun: "run-1", Authority: "authority-1 @ sha256:authority", Mandate: "mandate-1", Runtime: "hermes-agent", Inputs: `brief (string) = "inspect"`}},
		RejectedSubmissions: []FieldModel{{Label: "submission-rejected", Value: "invalid_input · brief is required"}},
	}}
	model := PageModel{Authenticated: true, Surface: SurfaceModel{Domain: DomainGraphs, Title: "Graphs", State: "ready", Authoritative: true, TotalCount: 1, Records: []RecordModel{record}, Inspector: &record, InspectorOpen: true}}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{"Exact Graph revision", "active", "Topology", "review-before-publish", "review → publish", "draft → draft", "loop-review r7 @ sha256:loop", "brief:string required=true", "Accepted submission snapshots", "snapshot-1 @ sha256:snapshot", "authority-1 @ sha256:authority", "mandate-1", "hermes-agent", `brief (string) = &#34;inspect&#34;`, "Durable rejected submissions", "submission-rejected", "fleet-wide submission truth", "never attributed to this Graph"} {
		if !strings.Contains(html, required) {
			t.Fatalf("Graph inspector missing %q: %s", required, html)
		}
	}
}

func TestLoopComposerRendersStructuredBoundedContractWithoutAuthorityInputs(t *testing.T) {
	var output bytes.Buffer
	model := PageModel{
		Authenticated: true,
		CSRF:          "csrf-loop",
		Surface:       SurfaceModel{Domain: DomainLoops, Title: "Loops"},
		LoopComposer: &LoopComposerModel{Publishers: []LoopPublisherModel{{
			ID: "agent-builder", Revision: "r2", Digest: "sha256:agent", Runtime: "hermes-agent",
		}}},
	}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{
		`action="/console/loops/preview"`, `name="csrf"`, `value="csrf-loop"`,
		`name="publisher_id"`, `value="agent-builder"`, `name="publication_key"`,
		`name="loop_id"`, `name="revision"`, `name="previous_digest"`, `name="entry_step_id"`,
		`name="inputs"`, `name="outputs"`, `name="steps"`, `name="step_ports"`,
		`name="terminal_mappings"`, `name="evidence_claims"`, `name="transitions"`,
		`name="transition_mappings"`, `name="required_evidence"`,
		"Preview creates no Loop record", "server resolves the exact current Agent revision",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("Loop composer missing %q: %s", required, html)
		}
	}
	for _, forbidden := range []string{`name="authority"`, `name="authority_id"`, `name="authority_digest"`, `name="stanza"`, `name="mandate"`, `name="principal"`} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("Loop composer exposed controller authority input %q: %s", forbidden, html)
		}
	}
}

func TestLoopLifecycleAndConfirmationRemainDigestBound(t *testing.T) {
	var output bytes.Buffer
	detail := &LoopDetailModel{
		TargetID: "loop.review:2", Digest: "sha256:revision", PublisherID: "agent-builder",
		ExpectedLifecycleDigest: "sha256:lifecycle", CanActivate: true, CanRetire: true,
	}
	record := RecordModel{Key: detail.TargetID, Label: "loop.review", Revision: "r2", Lifecycle: "published", Readiness: "Eligible", Loop: detail}
	model := PageModel{Authenticated: true, CSRF: "csrf-loop", Surface: SurfaceModel{
		Domain: DomainLoops, Title: "Loops", CSRF: "csrf-loop", State: "ready", Authoritative: true,
		Records: []RecordModel{record}, Inspector: &record, InspectorOpen: true,
	}}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{
		`action="/console/loops/lifecycle-preview"`, `name="target_id" value="loop.review:2"`,
		`name="expected_digest" value="sha256:revision"`, `name="expected_previous_digest" value="sha256:lifecycle"`,
		`name="publisher_id" value="agent-builder"`, `name="state" value="active"`, `name="state" value="retired"`,
		"Preview activation", "Preview retirement", "execute-time admission resolves the exact publisher and authority again",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("Loop lifecycle control missing %q: %s", required, html)
		}
	}

	output.Reset()
	model.Surface = SurfaceModel{Domain: DomainLoops, Title: "Loops"}
	model.CommandPreview = &CommandPreviewModel{IntentID: "intent-1", CommandID: "loop.lifecycle", TargetID: detail.TargetID, TargetDigest: detail.Digest, InputDigest: "sha256:input", ExpiresAt: "2026-08-22T12:00:00Z"}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html = output.String()
	for _, required := range []string{`action="/console/loops/execute"`, `name="intent_id" value="intent-1"`, "sha256:revision", "sha256:input", "Preview is non-persistent", "Confirm and execute"} {
		if !strings.Contains(html, required) {
			t.Fatalf("Loop confirmation missing %q: %s", required, html)
		}
	}
	if strings.Contains(html, `name="target_id"`) || strings.Contains(html, `name="authority_id"`) {
		t.Fatalf("confirmation allowed browser mutation of a retained target or authority: %s", html)
	}
}

func TestExecutionQueueDetailRendersAuthoritativeOrderAndNeverUpgradesSuccess(t *testing.T) {
	var output bytes.Buffer
	record := RecordModel{Key: "queue-130", Label: "queue-130", Summary: "graph-run-130 · failed", Lifecycle: "failed", Revision: "snapshot-130", Runtime: "hermes-agent", Queue: &QueueDetailModel{
		QueueItem: []FieldModel{{Label: "Queue item", Value: "queue-130 @ sha256:item"}, {Label: "Authority", Value: "authority-130 @ sha256:authority"}},
		Runtime:   []FieldModel{{Label: "Adapter", Value: "hermes"}, {Label: "Target", Value: "aegis-owned-ephemeral"}},
		GraphRun:  QueueExecutionNodeModel{ID: "graph-run-130", Kind: "Graph run", State: "succeeded", Binding: "snapshot-130 @ sha256:snapshot"},
		Loops:     []QueueExecutionNodeModel{{ID: "loop-exec-130", Kind: "Loop execution · review", State: "failed", Binding: "loop-review r7 @ sha256:loop"}},
		Attempts:  []QueueAttemptModel{{ID: "attempt-130", Number: 1, State: "failed", LoopID: "loop-exec-130", ClaimID: "claim-130", Created: "2026-08-18T12:00:00Z", Digest: "sha256:attempt"}},
		Timeline:  []QueueTimelineModel{{Title: "Queued", State: "queued", At: "2026-08-18T11:59:00Z", Detail: "queue-130"}, {Title: "Attempt 1", State: "failed", At: "2026-08-18T12:00:00Z", Detail: "attempt-130"}, {Title: "Disposition", State: "failed", At: "2026-08-18T12:01:00Z", Detail: "runtime_exit_nonzero"}},
		Artifact:  []FieldModel{{Label: "Artifact", Value: "artifact-130"}}, ArtifactState: "Authoritative runtime artifact",
		Receipts: []QueueReceiptModel{{ID: "receipt-130", Outcome: "passed", Claim: "review-receipt", Verifier: "artifact-verifier / v1"}}, ReceiptState: "Authoritative verifier receipts",
		Disposition: []FieldModel{{Label: "State", Value: "failed"}, {Label: "Reason code", Value: "runtime_exit_nonzero"}}, DispositionState: "Authoritative terminal disposition",
		Controls: []QueueControlModel{{Operation: "cancel", Label: "Cancel execution", Enabled: true, Reason: "eligible", Consequence: "Records an operator cancellation."}, {Operation: "retry", Label: "Retry active execution", Enabled: false, Reason: "runtime stop is unproven", Consequence: "Denied until Aegis proves runtime stop."}},
	}}
	model := PageModel{Authenticated: true, CSRF: "csrf-session", Surface: SurfaceModel{Domain: DomainQueue, Title: "Execution Queue", State: "ready", Authoritative: true, TotalCount: 1, QueueState: "failed", QueueStates: []string{"failed"}, FailedRecords: []RecordModel{record}, Records: []RecordModel{record}, Inspector: &record, InspectorOpen: true, CSRF: "csrf-session"}}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	ordered := []string{"Authoritative execution record", "Execution canvas", "Inspector", "Execution timeline", "Attempt provenance", "Runtime artifact", "Verifier receipts", "Terminal disposition"}
	position := -1
	for _, text := range ordered {
		next := strings.Index(html, text)
		if next <= position {
			t.Fatalf("execution detail order lost at %q", text)
		}
		position = next
	}
	for _, required := range []string{"queue-130", "#/queue/queue-130", "/console/queue?state=failed#/queue", "/console/queue?state=claimable#/queue", "graph-run-130", "loop-exec-130", "attempt-130", "claim-130", "artifact-130", "receipt-130", "runtime_exit_nonzero", "Only this authoritative disposition can support terminal success", "has not been upgraded"} {
		if !strings.Contains(html, required) {
			t.Fatalf("execution detail missing %q", required)
		}
	}
	for _, required := range []string{`method="post"`, `action="/console/queue/queue-130/operate"`, `name="csrf" value="csrf-session"`, `name="operation" value="cancel"`, `name="operation" value="retry"`, "Records an operator cancellation.", "Denied until Aegis proves runtime stop."} {
		if !strings.Contains(html, required) {
			t.Fatalf("execution lifecycle controls missing %q: %s", required, html)
		}
	}
	if strings.Count(html, `type="submit" class="secondary"`) != 2 || !strings.Contains(html, `value="cancel"><p><strong>Cancel execution`) || !strings.Contains(html, `value="retry"><p><strong>Retry active execution`) || !strings.Contains(html, `class="secondary" disabled data-eligible="false"`) {
		t.Fatalf("queue controls did not preserve exact eligibility: %s", html)
	}
	if strings.Contains(html, "Succeeded execution") || strings.Contains(html, "execution succeeded") {
		t.Fatalf("passing receipt or Graph run upgraded failed queue truth: %s", html)
	}
}

func TestAuthenticationAcceptsOnlyPrincipalPassword(t *testing.T) {
	var output bytes.Buffer
	if err := Authentication(AuthenticationModel{}).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{
		"Sign the Aegis principal into this browser",
		"principal configured when Aegis was initialized",
		"does not create or change the principal, tenant, or authority context",
		"browser cannot select a principal, actor, tenant, trust stanza, mandate, or authority",
		"does not provision identity or grant authority",
		"principal password",
		"credential-authority passphrase",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("authentication guidance missing %q: %s", required, html)
		}
	}
	for _, forbidden := range []string{`name="principal"`, `name="actor"`, `name="tenant"`, `name="stanza"`, `name="mandate"`, `name="authority"`} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("browser authentication exposes authority-bearing input %q: %s", forbidden, html)
		}
	}
	if strings.Count(html, `<input`) != 1 || !strings.Contains(html, `name="password"`) || !strings.Contains(html, `autocomplete="current-password"`) {
		t.Fatalf("browser authentication must accept exactly one principal-password input: %s", html)
	}
}

func TestCredentialsRendersActiveAndRevokedWithoutCiphertextLeakage(t *testing.T) {
	var output bytes.Buffer
	active := RecordModel{
		Key: "secret-1", Label: "github/api", Revision: "v2", Lifecycle: "active", Summary: "github/api · api-token",
		Source: "credentials.authority.bbolt", Owner: "operator",
		Fields: []FieldModel{{Label: "Stable record ID", Value: "github/api"}, {Label: "Reference", Value: "github/api"}, {Label: "Status", Value: "active"}, {Label: "Current version", Value: "v2"}, {Label: "Bindings", Value: "2 credential binding(s)"}, {Label: "Version history", Value: "v1: algorithm xchacha20-poly1305 · KEK v1 · digest sha256:aaa · created 2026-08-18T12:00:00Z\nv2: algorithm xchacha20-poly1305 · KEK v1 · digest sha256:bbb · created 2026-08-18T13:00:00Z"}},
		Credential: &CredentialDetailModel{
			Reference: "github/api", Kind: "api-token", Status: "active", CurrentVersion: 2,
			CreatedAt: "2026-08-18T12:00:00Z", CreatedBy: "principal-1", BindingCount: 2,
			Versions: []CredentialVersionDetail{
				{Version: 1, Algorithm: "xchacha20-poly1305", KEKVersion: 1, CiphertextHash: "sha256:aaa", CreatedAt: "2026-08-18T12:00:00Z"},
				{Version: 2, Algorithm: "xchacha20-poly1305", KEKVersion: 1, CiphertextHash: "sha256:bbb", CreatedAt: "2026-08-18T13:00:00Z"},
			},
			Vault:    CredentialVaultDetail{DeploymentID: "deployment-test", StoreID: "store-1", KEKID: "kek-1", KEKVersion: 1, SchemaVersion: "1", Custody: "host-file", LastCleanShutdown: true, State: "initialized", ReasonCode: "credentials_vault_ready"},
			Backup:   CredentialBackupDetail{Available: false, Note: "Backups are ciphertext-only snapshots; the same KEK is required to reopen."},
			Proposal: CredentialProposalDetail{PutCommand: "aegis secret put github/api --kind api-token --created-by \"$OPERATOR\"", BackupCommand: "aegis secret backup", Notice: "Browser state cannot authorize credential mutation."},
		},
	}
	revoked := RecordModel{
		Key: "secret-2", Label: "github/legacy", Revision: "v1", Lifecycle: "revoked", Summary: "github/legacy · api-token",
		Source: "credentials.authority.bbolt", Owner: "operator",
		Fields: []FieldModel{{Label: "Stable record ID", Value: "github/legacy"}, {Label: "Status", Value: "revoked"}, {Label: "Revoked at", Value: "2026-08-18T14:00:00Z"}, {Label: "Revocation reason", Value: "rotation"}},
		Credential: &CredentialDetailModel{
			Reference: "github/legacy", Status: "revoked", CurrentVersion: 1, RevokedAt: "2026-08-18T14:00:00Z", Revocation: "rotation",
			Vault:    CredentialVaultDetail{State: "initialized", ReasonCode: "credentials_vault_ready"},
			Proposal: CredentialProposalDetail{PutCommand: "aegis secret put github/legacy --kind api-token", BackupCommand: "aegis secret backup", Notice: "Browser state cannot authorize credential mutation."},
		},
	}
	active.JSON = `{"id":"secret-1","status":"active","version_history":[],"vault":{"kek_id":"kek-1","kek_version":1}}`
	revoked.JSON = `{"id":"secret-2","status":"revoked","version_history":[],"vault":{"kek_id":"kek-1","kek_version":1}}`
	model := PageModel{Authenticated: true, Surface: SurfaceModel{
		Domain: DomainCredentials, Title: "Credentials", Eyebrow: "Encrypted credential authority",
		Description: "Authoritative encrypted credential records.",
		State:       "ready", Authoritative: true, TotalCount: 2, Lifecycle: "all", Query: "",
		Records: []RecordModel{active, revoked}, Inspector: &revoked, InspectorOpen: true,
	}}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{
		"Encrypted credential authority", "Authoritative encrypted credential", "github/api", "github/legacy",
		"active", "revoked", "v2", "v1", "Revoked at", "Revocation reason", "rotation",
		"Vault summary", "Version history (encrypted, metadata-only)", "Prepare credential (review only)",
		"Prepare vault backup (review only)", "aegis secret put github/legacy", "aegis secret backup",
		"Browser state cannot authorize credential mutation",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("credential surface missing %q: %s", required, html)
		}
	}
	for _, forbidden := range []string{
		"<script", "data-on:", "data-bind:", "localStorage", "sessionStorage",
		`name="principal"`, `name="stanza"`, `name="mandate"`, `name="authority"`,
		"Ciphertext\":", "WrappedDEK\":", "RecordNonce\":", "WrapNonce\":",
		"source_env", "target_env",
		"secret backup <", "secret backup --path", "secret backup /", "secret backup ./", "secret backup ../",
	} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("credential surface exposed %q: %s", forbidden, html)
		}
	}
}

func TestCredentialsWorkspaceReportsUnconfiguredAuthorityWithoutAssertingCount(t *testing.T) {
	var output bytes.Buffer
	model := PageModel{Authenticated: true, Surface: SurfaceModel{
		Domain: DomainCredentials, Title: "Credentials", Eyebrow: "Encrypted credential authority",
		Description: "Authoritative encrypted credential records.",
		State:       "unconfigured", Authoritative: false, TotalCount: 0, ReasonCode: "credentials_authority_not_configured", Source: "credentials.authority.unconfigured",
		Records: []RecordModel{},
	}}
	if err := Document(model).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if !strings.Contains(html, "credentials_authority_not_configured") {
		t.Fatalf("unconfigured credentials surface must report reason: %s", html)
	}
	if strings.Contains(html, "0 credentials") {
		t.Fatalf("unconfigured surface should not assert a count: %s", html)
	}
}

func TestConsoleSourceForbidsUnsafeActiveContent(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source")
	}
	directory := filepath.Dir(file)
	for _, name := range []string{"components.templ", "model.go"} {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, forbidden := range []string{"templ.Raw", "SafeURL", "SafeCSS", "ExecuteScript", "innerHTML", "outerHTML", "document.write", "eval(", "new Function", "localStorage", "sessionStorage", "<script>"} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s contains prohibited active-content primitive %q", name, forbidden)
			}
		}
	}
}
