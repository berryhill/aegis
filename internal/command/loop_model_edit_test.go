package command

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
)

func TestOwnedLoopModelEditCLIUsesOwningServiceWithoutFiles(t *testing.T) {
	root, err := os.MkdirTemp(os.TempDir(), "loop-edit-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	cfg := config.Defaults()
	t.Setenv("AEGIS_API_LISTEN", cfg.API.Listen)
	t.Setenv("AEGIS_API_CONSOLE_ORIGIN", cfg.API.Console.Origin)
	cfg.StateDir = filepath.Join(root, "must-not-open")
	cfg.API.UnixSocket = filepath.Join(root, "owner.sock")
	cfg.API.Token = "fixture-transport"
	cfg.Principal.UID, cfg.Principal.User = "1000", "fixture"
	listener, err := net.Listen("unix", cfg.API.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []string
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cfg.API.Token {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v1/config" {
			_ = json.NewEncoder(w).Encode(config.Redacted(cfg))
			return
		}
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Method+" "+r.URL.EscapedPath())
		switch r.URL.Path {
		case "/v1/loops/owned/1/model-edit":
			var in app.OwnedLoopModelUpdateInput
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			if in.AgentID != "owner" || in.Provider != "openai-codex" || in.Model != "gpt-6.2-sol" || !reflect.DeepEqual(in.Steps, []string{"implement", "completion"}) {
				t.Errorf("wrong typed selection: %+v", in)
			}
			w.WriteHeader(http.StatusAccepted)
		case "/v1/loops/edits/retained/publish":
			raw, _ := io.ReadAll(r.Body)
			if len(raw) != 0 {
				t.Error("resume sends editable payload")
			}
		}
		_, _ = io.WriteString(w, `{"fixture_adapter":true}`)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { _ = server.Close() })
	raw, _ := json.Marshal(cfg)
	cfgFile := filepath.Join(root, "config.json")
	if err = os.WriteFile(cfgFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) error {
		var out bytes.Buffer
		cmd := NewRoot(Dependencies{Out: &out, Err: io.Discard})
		cmd.SetArgs(append([]string{"--config", cfgFile, "--target", cfg.API.Console.Origin, "loops"}, args...))
		return cmd.ExecuteContext(context.Background())
	}
	for _, args := range [][]string{{"model-update", "owned", "1", "--agent", "owner", "--provider", "openai-codex", "--model", "gpt-6.2-sol", "--step", "implement", "--step", "completion"}, {"edit-show", "retained"}, {"edit-publish", "retained"}} {
		if err = run(args...); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	actual := append([]string(nil), seen...)
	mu.Unlock()
	if !reflect.DeepEqual(actual, []string{"POST /v1/loops/owned/1/model-edit", "GET /v1/loops/edits/retained", "POST /v1/loops/edits/retained/publish"}) {
		t.Fatalf("routes: %v", actual)
	}
	if err = run("model-update", "owned", "1", "--agent", "owner", "--provider", "openai-codex"); err == nil {
		t.Fatal("missing selected model accepted")
	}
	if _, err = os.Stat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatal("CLI opened local stores")
	}
}
