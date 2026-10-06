package command

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/config"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This exercises the owning-service Unix adapter, not live runtime admission.
func TestDoerOnlineServicesUseOwningUnixTransport(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.StateDir = filepath.Join(root, "must-not-exist")
	cfg.API.UnixSocket = filepath.Join(root, "owner.sock")
	cfg.API.Token = "synthetic-transport-token"
	cfg.Principal.UID = "1000"
	cfg.Principal.User = "synthetic"
	listener, err := net.Listen("unix", cfg.API.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	var requests []string
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cfg.API.Token {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v1/config" {
			json.NewEncoder(w).Encode(config.Redacted(cfg))
			return
		}
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				w.WriteHeader(400)
				return
			}
			if strings.HasSuffix(r.URL.Path, "readiness") && body["probe"] != true {
				w.WriteHeader(400)
				return
			}
		}
		io.WriteString(w, `{"adapter":"owning-service"}`)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	cfgFile := filepath.Join(root, "config.json")
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name, body, path string
		extra            []string
	}{
		{"readiness", `{}`, "POST /v1/loops/doer/readiness", []string{"--probe"}},
		{"draft-save", `{}`, "POST /v1/loops/doer/drafts", nil},
		{"draft-show", "example", "GET /v1/loops/doer/drafts/example", nil},
		{"draft-continue", `{"id":"example","expected_version":1}`, "POST /v1/loops/doer/drafts/example/continue", nil},
		{"setup-protected", `{"id":"example","expected_version":1,"action":"host"}`, "POST /v1/loops/doer/drafts/example/setup-protected", nil},
		{"setup-review", `{"id":"example","expected_version":1}`, "POST /v1/loops/doer/drafts/example/setup-review", nil},
		{"setup-decide", `{"id":"example","receipt":"review","decision":"reject"}`, "POST /v1/loops/doer/drafts/example/setup-decision", nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			arg := row.body
			if row.name != "draft-show" {
				arg = filepath.Join(root, row.name+".json")
				if err := os.WriteFile(arg, []byte(row.body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			cmd := NewRoot(Dependencies{Out: &out, Err: io.Discard})
			args := []string{"--config", cfgFile, "--target", cfg.API.Console.Origin, "loops", row.name, arg}
			cmd.SetArgs(append(args, row.extra...))
			if err := cmd.ExecuteContext(context.Background()); err != nil || !strings.Contains(out.String(), "owning-service") {
				t.Fatalf("output=%s err=%v", out.String(), err)
			}
			if requests[len(requests)-1] != row.path {
				t.Fatalf("wrong route: %v", requests)
			}
		})
	}
	if _, err := os.Stat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatalf("alternate store created: %v", err)
	}
}
