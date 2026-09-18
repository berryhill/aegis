package command

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
)

// The request contracts below mirror the existing strict handlers in api/server.go.
// This transport fixture is not live provider acceptance.
func TestOnlinePreparationContracts(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.StateDir = filepath.Join(root, "unopened")
	cfg.API.UnixSocket = filepath.Join(root, "owner.sock")
	cfg.API.Token = "synthetic"
	cfg.Principal.UID, cfg.Principal.User = "1000", "synthetic"
	listener, err := net.Listen("unix", cfg.API.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	type call struct{ method, path, body string }
	calls := make(chan call, 1)
	mismatch, denied := false, false
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cfg.API.Token || denied {
			w.WriteHeader(403)
			return
		}
		if r.URL.Path == "/v1/config" {
			actual := config.Redacted(cfg)
			if mismatch {
				actual.StateDir += "other"
			}
			json.NewEncoder(w).Encode(actual)
			return
		}
		data, _ := io.ReadAll(r.Body)
		calls <- call{r.Method, r.URL.RequestURI(), string(data)}
		io.WriteString(w, `{}`)
	})}
	go server.Serve(listener)
	defer server.Close()
	raw, _ := json.Marshal(cfg)
	file := filepath.Join(root, "config.json")
	os.WriteFile(file, raw, 0600)
	charter := filepath.Join(root, "charter.yaml")
	os.WriteFile(charter, []byte("schema_version: 1\n"), 0600)
	run := func(args ...string) error {
		cmd := NewRoot(Dependencies{Out: io.Discard, Err: io.Discard})
		cmd.SetArgs(append([]string{"--config", file, "--target", cfg.API.Console.Origin}, args...))
		return cmd.Execute()
	}
	for _, tc := range []struct {
		args               []string
		method, path, body string
	}{
		{[]string{"charter", "import", charter}, "POST", "/v1/charters/import", "schema_version: 1\n"},
		{[]string{"charter", "validate", charter}, "POST", "/v1/charters/validate", "schema_version: 1\n"},
		{[]string{"charter", "show", "agent", "2"}, "GET", "/v1/charters/agent/2", ""},
		{[]string{"plan", "preview", "agent", "--revision", "2", "--environment", "local"}, "POST", "/v1/plans/preview", `{"agent":"agent","revision":2,"environment":{"name":"local"}}`},
		{[]string{"plan", "show", "plan"}, "GET", "/v1/plans/plan", ""},
		{[]string{"approval", "request", "plan", "--ttl", "2m"}, "POST", "/v1/approvals", `{"plan_id":"plan","ttl":"2m0s"}`},
		{[]string{"approval", "approve", "approval"}, "POST", "/v1/approvals/approval/decision", `{"approve":true}`},
		{[]string{"approval", "reject", "approval"}, "POST", "/v1/approvals/approval/decision", `{"approve":false}`},
		{[]string{"approval", "show", "approval"}, "GET", "/v1/approvals/approval", ""},
		{[]string{"provision", "plan", "approval"}, "POST", "/v1/provision", `{"plan_id":"plan","approval_id":"approval"}`},
		{[]string{"session", "preview", "agent", "--revision", "2", "--stanza", "operator"}, "POST", "/v1/sessions/preview", `{"agent":"agent","revision":2,"environment":{"name":"local"},"stanza":"operator"}`},
		{[]string{"session", "start", "mandate"}, "POST", "/v1/sessions/start", `{"mandate_id":"mandate"}`},
		{[]string{"session", "show", "session"}, "GET", "/v1/sessions/session", ""},
		{[]string{"session", "authority", "session"}, "GET", "/v1/sessions/session/authority", ""},
		{[]string{"session", "list"}, "GET", "/v1/sessions", ""},
	} {
		t.Run(strings.Join(tc.args, "-"), func(t *testing.T) {
			if err := run(tc.args...); err != nil {
				t.Fatal(err)
			}
			got := <-calls
			if got.method != tc.method || got.path != tc.path {
				t.Fatalf("route %+v", got)
			}
			if strings.HasPrefix(tc.body, "{") {
				var a, b any
				json.Unmarshal([]byte(got.body), &a)
				json.Unmarshal([]byte(tc.body), &b)
				if !reflect.DeepEqual(a, b) {
					t.Fatalf("payload %s != %s", got.body, tc.body)
				}
			} else if got.body != tc.body {
				t.Fatalf("raw payload %q != %q", got.body, tc.body)
			}
		})
	}
	mismatch = true
	if err := run("provision", "plan", "approval"); err == nil || !strings.Contains(err.Error(), "owning_instance_mismatch") {
		t.Fatalf("mismatch: %v", err)
	}
	mismatch, denied = false, true
	if err := run("approval", "approve", "approval"); err == nil || !strings.Contains(err.Error(), "owning_service_denied") {
		t.Fatalf("denial: %v", err)
	}
	select {
	case got := <-calls:
		t.Fatalf("unauthorized mutation %+v", got)
	default:
	}
	if _, err := os.Stat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatalf("local state opened: %v", err)
	}
}
