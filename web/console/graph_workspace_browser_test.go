package consoleweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Opt-in real Chromium component proof. This is not authenticated API or installed-binary proof.
func TestGraphWorkspaceBrowser(t *testing.T) {
	if os.Getenv("AEGIS_GRAPH_BROWSER_TEST") != "1" {
		t.Skip("set AEGIS_GRAPH_BROWSER_TEST=1 with Python Playwright installed")
	}
	python := os.Getenv("AEGIS_GRAPH_BROWSER_PYTHON")
	if python == "" {
		python = "python3"
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Fatalf("%s not on PATH", python)
	}
	if _, err := os.Stat("../../scripts/graph_workspace_browser_test.py"); err != nil {
		t.Fatal(err)
	}
	g := &GraphDetailModel{GraphID: "graph-proof", LatestVersion: "r2 · viewing historical revision", Validation: "valid · stored", CurrentValidation: "valid", Nodes: []GraphNodeModel{{ID: "intake", Participant: "agent r1", Loop: "loop r1"}, {ID: "review", Participant: "agent r1", Loop: "loop r2"}}, Edges: []GraphEdgeModel{{ID: "next", From: "intake", To: "review"}}}
	r := &RecordModel{Key: "graph-proof:1", Label: "graph-proof", Revision: "r1", Lifecycle: "draft", Readiness: "Not submittable without fresh admission", Graph: g}
	body := strings.Builder{}
	if err := GraphDetailPage(SurfaceModel{Domain: DomainGraphs}, r).Render(context.Background(), &body); err != nil {
		t.Fatal(err)
	}
	shell := `<!doctype html><html><head><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/app.css"><script defer src="/graph.js"></script></head><body>` + body.String() + `</body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/app.css":
			w.Header().Set("Content-Type", "text/css")
			w.Write(CSS)
		case "/graph.js":
			w.Header().Set("Content-Type", "text/javascript")
			w.Write(graphWorkspaceJS)
		default:
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; form-action 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'")
			w.Write([]byte(shell))
		}
	}))
	defer server.Close()
	command := exec.CommandContext(context.Background(), python, "../../scripts/graph_workspace_browser_test.py", server.URL)
	output, err := command.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatal(err)
	}
}
