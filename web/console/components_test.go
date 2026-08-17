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
	for _, required := range []string{"<nav", "<main", "aria-live", `id="authentication-status"`, `data-state="loading"`, `data-state="empty"`, `data-state="denied"`, `data-state="unavailable"`, `data-state="degraded_repair_required"`, `data-state="error"`, `method="post"`, `action="/console/session"`} {
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

func TestAuthenticationExplainsAuthenticatedHostBootstrapHandoff(t *testing.T) {
	var output bytes.Buffer
	if err := Authentication("").Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{
		"Sign the Aegis principal into this browser",
		"principal configured when Aegis was initialized",
		"does not create or change the principal, tenant, or authority context",
		"On the Aegis host, open a terminal as the initialized principal",
		"aegis console",
		"authenticates the host user and creates a temporary, single-use browser handoff",
		"browser cannot select a principal, actor, tenant, trust stanza, mandate, or authority",
		"only hands the existing authenticated principal into this browser",
		"does not provision identity or grant authority",
		"submit promptly",
		"without launching a browser",
		"bare production",
		"aegis service start",
		"If the bootstrap expires",
		"Never paste an API bearer",
		"credential in a URL",
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
	if strings.Count(html, `<input`) != 1 || !strings.Contains(html, `name="bootstrap"`) {
		t.Fatalf("browser authentication must accept exactly one bootstrap input: %s", html)
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
