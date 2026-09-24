package manager

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in, credential-free probe of the exact manager gateway launch and turn.
// The server never forwards to a model and the disposable runtime home is removed.
func TestInstalledManagerGatewayProtocol(t *testing.T) {
	installation := os.Getenv("AEGIS_TEST_HERMES_INSTALLATION")
	root := os.Getenv("AEGIS_TEST_GATEWAY_ROOT")
	if installation == "" || root == "" {
		t.Skip("installed Hermes and isolated probe root required")
	}
	if !filepath.IsAbs(installation) || !filepath.IsAbs(root) {
		t.Fatal("absolute paths required")
	}
	python := filepath.Join(installation, "venv", "bin", "python")
	if _, err := os.Stat(python); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"probe\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"qwen3.5:4b\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"{\\\"kind\\\":\\\"message\\\",\\\"message\\\":\\\"safe\\\",\\\"proposal\\\":null,\\\"reason\\\":null}\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"probe\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"qwen3.5:4b\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	process, err := StartHermesProcess(ctx, HermesProcessConfig{Python: python, Installation: installation, StateRoot: root, ProxyEndpoint: server.URL, Model: "qwen3.5:4b", MaximumMessageBytes: 1 << 20, StartTimeout: 30 * time.Second, AuthorizeRelease: authorizeTestHermesRelease(t)})
	if err != nil {
		t.Fatalf("gateway startup: %v", err)
	}
	defer func() {
		closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if err := process.Close(closeCtx); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	}()
	session, err := process.Client().CreateSession(ctx, "aegis-manager-certification")
	if err != nil {
		t.Fatalf("session.create: %v", err)
	}
	output, err := process.Client().Turn(ctx, session, "Return a safe message JSON envelope", 1<<20)
	if err != nil {
		t.Fatalf("prompt.submit: %v", err)
	}
	if !strings.Contains(string(output), `"kind":"message"`) {
		t.Fatalf("unexpected completion length=%d", len(output))
	}
	second, err := process.Client().Turn(ctx, session, "Return another safe message JSON envelope", 1<<20)
	if err != nil {
		t.Fatalf("second prompt.submit on settled session: %v", err)
	}
	if !strings.Contains(string(second), `"kind":"message"`) {
		t.Fatalf("unexpected second completion length=%d", len(second))
	}
}
