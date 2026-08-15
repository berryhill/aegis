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
	for _, required := range []string{"<nav", "<main", "aria-live", `id="authentication-status"`, `data-state="loading"`, `data-state="empty"`, `data-state="unavailable"`, `data-state="error"`, `method="post"`, `action="/console/session"`} {
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
		`href="/console?domain=loops"`, `href="/console?domain=graphs"`,
		`href="/console?domain=queue"`, `record_key=0`, `id="close-inspector"`,
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("native interaction missing %q: %s", required, html)
		}
	}
	if strings.Contains(html, "data-on:") || strings.Contains(html, "data-signals") {
		t.Fatal("authenticated document still depends on runtime expression evaluation")
	}
}

func TestAuthenticationExplainsAuthenticatedHostBootstrapHandoff(t *testing.T) {
	var output bytes.Buffer
	if err := Authentication("").Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	for _, required := range []string{
		"Open a terminal on the Aegis host",
		"aegis console",
		"Copy only the short-lived, single-use bootstrap",
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
