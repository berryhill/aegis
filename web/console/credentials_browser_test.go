package consoleweb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
)

// Opt-in real Chrome interaction proof using synthetic metadata, never custody.
func TestCredentialWorkspaceBrowser(t *testing.T) {
	python := os.Getenv("AEGIS_BROWSER_PYTHON")
	if python == "" {
		t.Skip("set AEGIS_BROWSER_PYTHON to a Python with Playwright installed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/console/assets/app.css":
			w.Header().Set("Content-Type", "text/css")
			w.Write(CSS)
			return
		case "/console/assets/navigation.js":
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(NavigationJS)
			return
		case "/console/credentials":
		default:
			http.NotFound(w, r)
			return
		}
		s := SurfaceModel{Domain: DomainCredentials, Title: "Credentials", Authoritative: true, State: "ready", TotalCount: 80, Query: r.URL.Query().Get("q"), Lifecycle: r.URL.Query().Get("status"), CollectionURL: "/console/credentials?q=provider&status=active#/credentials"}
		for i := 0; i < 80; i++ {
			key := fmt.Sprintf("secret-%02d", i)
			c := &CredentialDetailModel{ID: key, Reference: "provider/test", Kind: "provider-token", Status: "active", CurrentVersion: 80}
			for v := 1; v <= 80; v++ {
				c.Versions = append(c.Versions, CredentialVersionDetail{Version: uint64(v), CiphertextHash: "sha256:synthetic"})
			}
			s.Records = append(s.Records, RecordModel{Key: key, Revision: "v80", Lifecycle: "active", Credential: c})
		}
		key := r.URL.Query().Get("record_key")
		s.InspectorOpen = key != ""
		for i := range s.Records {
			if s.Records[i].Key == key {
				s.Inspector = &s.Records[i]
			}
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		if err := Document(PageModel{Authenticated: true, Surface: s}).Render(context.Background(), w); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	cmd := exec.Command(python, "testdata/credentials_browser.py", server.URL)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
}
