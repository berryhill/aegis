package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/spf13/cobra"
)

func TestLoopAvailabilityCLIInput(t *testing.T) {
	for _, action := range []string{"archive", "restore"} {
		t.Run(action, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.json")
			if err := os.WriteFile(path, []byte(`{"head":{"id":"loop"},"state":"wrong"}`), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readLoopAvailabilityInput("loop", path, action)
			expected := loop.AvailabilityArchived
			if action == "restore" {
				expected = loop.AvailabilityPublished
			}
			if err != nil || got.State != expected {
				t.Fatalf("state: %+v %v", got, err)
			}
			for _, raw := range []string{`{"head":{"id":"wrong"}}`, `{"head":{"id":"loop"},"unknown":true}`, `{"head":{"id":"loop"}} {}`} {
				if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
				cmd := fleetLoopsCmd(func(*cobra.Command) (*app.Service, error) { t.Fatal("invalid input reached service"); return nil, nil })
				cmd.SetArgs([]string{action, "loop", path})
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				if err := cmd.Execute(); err == nil {
					t.Fatalf("accepted %s", raw)
				}
			}
		})
	}
	// Local read adapters and valid mutations reach the local service builder.
	sentinel := errors.New("local builder reached")
	for _, args := range [][]string{{"inventory"}, {"availability", "loop"}} {
		cmd := fleetLoopsCmd(func(*cobra.Command) (*app.Service, error) { return nil, sentinel })
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); !errors.Is(err, sentinel) {
			t.Fatalf("local %v: %v", args, err)
		}
	}
}

func TestLoopAvailabilityCLIOnline(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.StateDir = filepath.Join(root, "must-not-exist")
	cfg.API.UnixSocket = filepath.Join(root, "owner.sock")
	cfg.API.Token = "synthetic-transport-token"
	cfg.Principal.UID = "1000"
	cfg.Principal.User = "synthetic"
	var seen []string
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
		seen = append(seen, r.Method+" "+r.URL.EscapedPath())
		if r.Method == http.MethodPut {
			var in app.SetLoopAvailabilityInput
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			expected := loop.AvailabilityArchived
			if len(seen) == 4 {
				expected = loop.AvailabilityPublished
			}
			if in.State != expected || in.Head.ID != "loop/one" || in.AgentID != "owner" {
				t.Errorf("payload: %+v", in)
			}
		}
		io.WriteString(w, `{}`)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	cfgFile := filepath.Join(root, "config.json")
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "input.json")
	if err := os.WriteFile(file, []byte(`{"agent_id":"owner","head":{"id":"loop/one"},"state":"wrong"}`), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) error {
		var out bytes.Buffer
		cmd := NewRoot(Dependencies{Out: &out, Err: io.Discard})
		cmd.SetArgs(append([]string{"--config", cfgFile, "--target", cfg.API.Console.Origin, "loops"}, args...))
		return cmd.ExecuteContext(context.Background())
	}
	for _, args := range [][]string{{"inventory"}, {"availability", "loop/one"}, {"archive", "loop/one", file}, {"restore", "loop/one", file}} {
		if err := run(args...); err != nil {
			t.Fatalf("online %v: %v", args, err)
		}
	}
	expected := []string{"GET /v1/loops/inventory", "GET /v1/loops/loop%2Fone/availability", "PUT /v1/loops/loop%2Fone/availability", "PUT /v1/loops/loop%2Fone/availability"}
	if len(seen) != len(expected) {
		t.Fatalf("requests: %v", seen)
	}
	for i := range expected {
		if seen[i] != expected[i] {
			t.Fatalf("route %d: %s", i, seen[i])
		}
	}
	for _, raw := range []string{`{"agent_id":"owner","head":{"id":"wrong"}}`, `{"agent_id":"owner","head":{"id":"loop/one"},"unknown":true}`, `{"agent_id":"owner","head":{"id":"loop/one"}} {}`, `{"head":{"id":"loop/one"}}`, `{"agent_id":"owner","head":{"id":"loop/one"},"authority":{"id":"client"}}`} {
		if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"archive", "restore"} {
			if err := run(action, "loop/one", file); err == nil {
				t.Fatalf("accepted %s", raw)
			}
		}
	}
	if len(seen) != 4 {
		t.Fatalf("invalid input reached API: %v", seen)
	}
	if _, err := os.Stat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatalf("local state fallback: %v", err)
	}
}
