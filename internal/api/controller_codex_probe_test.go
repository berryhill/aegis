package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/reference"
)

func TestControllerCodexProbeRejectsOverridesWithoutEffects(t *testing.T) {
	s := apiService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, s) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", s.Config.API.UnixSocket)
	client := unixClient(s.Config.API.UnixSocket)
	var out map[string]any
	apiRequest(t, client, http.MethodPost, "/v1/controller/codex/probe", map[string]any{"prompt": "create result.txt"}, &out, http.StatusBadRequest)
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "synthetic", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
	apiRequest(t, client, http.MethodPost, "/v1/controller/codex/probe", app.ControllerCodexProbeInput{Agent: ref, Loop: ref, IdempotencyKey: "synthetic-no-connection"}, &out, http.StatusForbidden)
	if err := s.Store.List("controller-provider-probes", func(json.RawMessage) error {
		t.Fatal("denied probe reserved inference")
		return nil
	}); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if sessions, err := s.ListSessions(); err != nil || len(sessions) != 0 {
		t.Fatal("denied probe created a runtime session", err)
	}
}
