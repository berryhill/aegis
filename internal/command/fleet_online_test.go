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
	"strings"
	"sync/atomic"
	"testing"

	"github.com/berryhill/aegis/internal/config"
)

func TestFleetOnlineTargetRoutesWithoutOpeningState(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.StateDir = filepath.Join(root, "must-not-exist")
	cfg.API.UnixSocket = filepath.Join(root, "owner.sock")
	cfg.API.Token = "synthetic-transport-token"
	cfg.Principal.UID = "1000"
	cfg.Principal.User = "synthetic"
	calls := 0
	var trailing atomic.Bool
	listener, err := net.Listen("unix", cfg.API.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cfg.API.Token {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/v1/config":
			json.NewEncoder(w).Encode(config.Redacted(cfg))
			if trailing.Load() {
				io.WriteString(w, `{}`)
			}
		case "/v1/agents":
			calls++
			io.WriteString(w, `[{"revision":{"agent_id":"existing-builder"}}]`)
		default:
			w.WriteHeader(404)
		}
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	cfgFile := filepath.Join(root, "config.json")
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(target string) (string, error) {
		var out bytes.Buffer
		cmd := NewRoot(Dependencies{Out: &out, Err: io.Discard})
		cmd.SetArgs([]string{"--config", cfgFile, "--target", target, "agents", "list"})
		err := cmd.ExecuteContext(context.Background())
		return out.String(), err
	}
	got, err := run(cfg.API.Console.Origin + "/console/agents#/agents")
	if err != nil || !strings.Contains(got, "existing-builder") {
		t.Fatalf("online read: %s %v", got, err)
	}
	if _, err := run("http://127.0.0.1:9999/console/agents"); err == nil {
		t.Fatal("wrong target admitted")
	}
	trailing.Store(true)
	if _, err := run(cfg.API.Console.Origin); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("trailing response admitted: %v", err)
	}
	for _, flag := range []string{"--update", "--pinentry-executable=/not-used", "--state-dir=/not-used", "--runtime=hermes"} {
		cmd := NewRoot(Dependencies{Out: io.Discard, Err: io.Discard})
		cmd.SetArgs([]string{"--config", cfgFile, "--target", cfg.API.Console.Origin, flag, "agents", "list"})
		if err := cmd.ExecuteContext(context.Background()); err == nil {
			t.Fatalf("override admitted: %s", flag)
		}
	}
	if calls != 1 {
		t.Fatalf("wrong target reached Agent API: %d", calls)
	}
	if _, err := os.Stat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatalf("created alternate state: %v", err)
	}
}
