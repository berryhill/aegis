package command

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
)

// Synthetic transport proof only: this does not establish runtime admission or
// independent execution evidence from a real owning service.
func TestFleetOnlineExecutionRoutes(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.StateDir = filepath.Join(root, "must-not-exist")
	cfg.API.UnixSocket = filepath.Join(root, "owner.sock")
	cfg.API.Token = "synthetic-token"
	cfg.Principal.UID, cfg.Principal.User = "1000", "synthetic"
	listener, err := net.Listen("unix", cfg.API.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	type call struct {
		method, path string
		body         map[string]any
	}
	calls := make(chan call, 20)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cfg.API.Token {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v1/config" {
			json.NewEncoder(w).Encode(config.Redacted(cfg))
			return
		}
		var body map[string]any
		if r.Method != http.MethodGet {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		calls <- call{r.Method, r.URL.Path, body}
		io.WriteString(w, `{}`)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	cfgFile := filepath.Join(root, "config.json")
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args []string, body string) error {
		file := filepath.Join(root, "input.json")
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		argv := []string{"--config", cfgFile, "--target", cfg.API.Console.Origin}
		for _, arg := range args {
			if arg == "FILE" {
				arg = file
			}
			argv = append(argv, arg)
		}
		cmd := NewRoot(Dependencies{Out: io.Discard, Err: io.Discard})
		cmd.SetArgs(argv)
		return cmd.Execute()
	}
	for _, tc := range []struct {
		args                      []string
		body, method, path, state string
	}{
		{[]string{"loops", "activate", "synthetic-loop", "FILE"}, `{"agent_id":"synthetic-agent","loop":{"id":"synthetic-loop"}}`, "PUT", "/v1/loops/synthetic-loop/lifecycle", "active"},
		{[]string{"loops", "retire", "synthetic-loop", "FILE"}, `{"agent_id":"synthetic-agent","loop":{"id":"synthetic-loop"}}`, "PUT", "/v1/loops/synthetic-loop/lifecycle", "retired"},
		{[]string{"graphs", "list"}, `{}`, "GET", "/v1/graphs", ""},
		{[]string{"graphs", "show", "synthetic-graph", "1"}, `{}`, "GET", "/v1/graphs/synthetic-graph/1", ""},
		{[]string{"graphs", "publish", "FILE"}, `{"agent_id":"synthetic-agent"}`, "POST", "/v1/graphs", ""},
		{[]string{"graphs", "submit", "FILE"}, `{"agent_id":"synthetic-agent"}`, "POST", "/v1/queue", ""},
		{[]string{"queue", "list"}, `{}`, "GET", "/v1/queue", ""},
		{[]string{"queue", "show", "synthetic-item"}, `{}`, "GET", "/v1/queue/synthetic-item", ""},
		{[]string{"queue", "process", "FILE"}, `{"queue_item_id":"synthetic-item"}`, "POST", "/v1/queue/synthetic-item/process", ""},
		{[]string{"queue", "bind-runtime", "FILE"}, `{"agent_id":"synthetic-agent","queue_item_id":"synthetic-item","authority":{"id":"runtime-authority"}}`, "POST", "/v1/queue/synthetic-item/bind-runtime", ""},
	} {
		t.Run(strings.Join(tc.args, "-"), func(t *testing.T) {
			if err := run(tc.args, tc.body); err != nil {
				t.Fatal(err)
			}
			got := <-calls
			if got.method != tc.method || got.path != tc.path {
				t.Fatalf("route: %+v", got)
			}
			if tc.path == "/v1/queue/synthetic-item/bind-runtime" {
				authority, ok := got.body["authority"].(map[string]any)
				if !ok || authority["id"] != "runtime-authority" || got.body["agent_id"] != "synthetic-agent" || got.body["queue_item_id"] != "synthetic-item" {
					t.Fatalf("runtime binding input changed: %+v", got.body)
				}
			}
			if tc.state != "" && got.body["state"] != tc.state {
				t.Fatalf("state: %+v", got.body)
			}
		})
	}
	for _, tc := range []struct {
		args []string
		body string
	}{
		{[]string{"graphs", "submit", "FILE", "--check"}, `{}`},
		{[]string{"graphs", "submit", "FILE"}, `{"workspace":{}}`},
		{[]string{"graphs", "publish", "FILE"}, `{"agent_id":"synthetic-agent","authority":{"id":"forged"}}`},
		{[]string{"graphs", "publish", "FILE"}, `{"agent_id":"synthetic-agent","unknown":true}`},
		{[]string{"graphs", "publish", "FILE"}, `{"agent_id":"synthetic-agent"} {}`},
		{[]string{"loops", "activate", "synthetic-loop", "FILE"}, `{"agent_id":"synthetic-agent","loop":{"id":"different"}}`},
		{[]string{"queue", "process", "FILE"}, `{}`},
		{[]string{"queue", "bind-runtime", "FILE"}, `{}`},
		{[]string{"queue", "bind-runtime", "FILE"}, `{"queue_item_id":"synthetic-item"}`},
		{[]string{"queue", "bind-runtime", "FILE"}, `{"agent_id":"synthetic-agent","queue_item_id":"synthetic-item","workspace":{}}`},
		{[]string{"queue", "bind-runtime", "FILE"}, `{"agent_id":"synthetic-agent","queue_item_id":"synthetic-item"} {}`},
	} {
		if err := run(tc.args, tc.body); err == nil {
			t.Fatalf("admitted invalid input: %v %s", tc.args, tc.body)
		}
		select {
		case got := <-calls:
			t.Fatalf("invalid input reached service: %+v", got)
		default:
		}
	}
	if _, err := os.Stat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatalf("opened local state: %v", err)
	}
}
