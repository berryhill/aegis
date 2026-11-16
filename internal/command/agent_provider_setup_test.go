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
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
)

func TestAgentProviderSetupCLITransport(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.StateDir = filepath.Join(root, "must-not-exist")
	cfg.API.UnixSocket = filepath.Join(socketTestDir(t), "provider.sock")
	cfg.API.Token = "synthetic-transport-token"
	cfg.Principal.UID = "1000"
	cfg.Principal.User = "synthetic"
	received := make(chan app.AgentProviderSetupInput, 16)
	listener, err := net.Listen("unix", cfg.API.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cfg.API.Token {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v1/config" {
			json.NewEncoder(w).Encode(config.Redacted(cfg))
			return
		}
		if r.URL.Path != "/v1/agents/exact/providers" || r.Method != "POST" {
			w.WriteHeader(404)
			return
		}
		var in app.AgentProviderSetupInput
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			w.WriteHeader(400)
			return
		}
		received <- in
		io.WriteString(w, `{"qualification":"not_checked","executable":false}`)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	cfgFile := filepath.Join(root, "owner.json")
	raw, _ := json.Marshal(cfg)
	if err = os.WriteFile(cfgFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"status"}, {"models"}, {"candidates", "/synthetic/.hermes"}, {"review", "/synthetic/.hermes", "nc-exact"}, {"connect", "review-exact"}, {"promote", "openai-codex", "gpt-5.4"}, {"approve", "review-exact", "sha256:exact"}} {
		expected, err := app.NewAgentProviderSetupInput(args)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		cmd := NewRoot(Dependencies{Out: &out, Err: io.Discard})
		cmd.SetArgs(append([]string{"--config", cfgFile, "--target", cfg.API.Console.Origin, "agents", "providers", "exact"}, args...))
		if err = cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := <-received; !reflect.DeepEqual(got, expected) {
			t.Fatalf("request changed: %+v", got)
		}
		if !bytes.Contains(out.Bytes(), []byte(`"executable": false`)) {
			t.Fatal("readiness output lost")
		}
	}
	if _, err = os.Stat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatal("online command opened alternate store")
	}
}
