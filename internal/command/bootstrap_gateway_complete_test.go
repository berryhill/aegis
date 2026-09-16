package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/config"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/onboarding"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type bootstrapApprovalInput struct {
	out               *bytes.Buffer
	endpoint, pending string
	launch            bool
}

func (r *bootstrapApprovalInput) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.pending == "" {
		text := r.out.String()
		if r.launch {
			return 0, io.EOF
		}
		if strings.HasSuffix(text, "Loopback endpoint [http://127.0.0.1:11434]: ") {
			r.pending = r.endpoint + "\n"
		} else {
			decision := text[strings.LastIndex(text, "DECISION /"):]
			r.pending = "yes\n"
			if strings.Contains(decision, "Review discovered Hermes") {
				r.pending = "no\n"
			}
			if strings.Contains(decision, "Start authenticated manager") {
				r.launch = true
			}
		}
	}
	p[0] = r.pending[0]
	r.pending = r.pending[1:]
	return 1, nil
}

// This fixture runs real certification and persistence with a synthetic Hermes
// protocol peer and Ollama. It is not live model/provider acceptance evidence.
func TestBootstrapGatewayCompleteApprovedResume(t *testing.T) {
	for _, entry := range []string{"init", "root"} {
		t.Run(entry, func(t *testing.T) {
			path, runner, _ := bootstrapGatewayFixture(t)
			root := filepath.Dir(path)
			t.Setenv("HOME", root)
			t.Setenv("HERMES_HOME", filepath.Join(root, "absent-hermes-home"))
			candidate := managerdomain.Candidates()[0]
			digest := strings.Repeat("c", 64)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/version":
					_, _ = w.Write([]byte(`{"version":"0.32.0"}`))
				case "/api/ps":
					_, _ = w.Write([]byte(`{"models":[]}`))
				case "/api/generate":
					_, _ = w.Write([]byte(`{"done":true}`))
				case "/api/tags":
					_, _ = fmt.Fprintf(w, `{"models":[{"name":%q,"model":%q,"digest":%q,"details":{"quantization_level":"Q4"}}]}`, candidate.OllamaName, candidate.OllamaName, digest)
				default:
					t.Errorf("unexpected Ollama effect: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			hermes := filepath.Join(root, "hermes")
			if err := os.WriteFile(hermes, []byte("#!/bin/sh\n[ \"$1\" = \"--version\" ] || exit 1\nprintf 'Hermes Agent v0.18.0\\nInstall directory: /test/hermes\\n'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			initial, err := config.Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			initial.Manager.Inference.Mode = "external-local"
			initial.Manager.Inference.Endpoint = server.URL
			managerJSON, err := json.Marshal(initial.Manager)
			if err != nil {
				t.Fatal(err)
			}
			document := string(mustCommandRead(t, path)) + fmt.Sprintf("hermes_executable: %q\nmanager: %s\n", hermes, managerJSON)
			if err := os.WriteFile(path, []byte(document), 0600); err != nil {
				t.Fatal(err)
			}
			if inspected := config.Inspect(path); inspected.State != config.StateValid {
				t.Fatalf("fixture config: state=%s reason=%s err=%v", inspected.State, inspected.ReasonCode, inspected.Err)
			}
			plan, err := onboarding.PreviewAuthority(path, "host-file")
			if err != nil {
				t.Fatal(err)
			}
			if err = onboarding.InitializeHostAuthority(context.Background(), plan); err != nil {
				t.Fatal(err)
			}
			if err = onboarding.ApplyAuthority(plan); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			authorityPath := filepath.Join(cfg.StateDir, "persistence", "authority-v1")
			// Hold the real operational store as a daemon would. The exact stop must
			// release it before bootstrap can inspect or construct a local service.
			authority, err := authoritybadger.Open(context.Background(), authorityPath)
			if err != nil {
				t.Fatal(err)
			}
			defer authority.Close()
			stop := runner.stop
			runner.stop = func() error { return errors.Join(stop(), authority.Close()) }

			responses := map[string]any{}
			for _, test := range managerdomain.ConformanceCorpus() {
				message := "safe"
				for _, group := range test.RequiredGroups {
					message += " " + group[0]
				}
				var proposal any
				if test.ExpectedOperation != "" {
					args := map[string]string{}
					for k, v := range test.ExpectedArguments {
						args[k] = v
					}
					proposal = map[string]any{"operation": test.ExpectedOperation, "arguments": args}
				}
				responses[test.ID] = map[string]any{"schema_version": managerdomain.ResponseSchemaVersion, "kind": test.ExpectedKind, "message": message, "proposal": proposal}
			}
			encoded, _ := json.Marshal(responses)
			script := `#!/usr/bin/python3
import sys,json,re
responses=json.loads(` + fmt.Sprintf("%q", string(encoded)) + `)
def emit(v): print(json.dumps(v),flush=True)
emit({"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}})
for line in sys.stdin:
 r=json.loads(line)
 if r.get("method")=="session.create":
  emit({"jsonrpc":"2.0","id":r["id"],"result":{"session_id":"fixture"}})
 elif r.get("method")=="prompt.submit":
  match=re.search(r"Case: ([^\n]+)",json.dumps(r).replace("\\n","\n"))
  response=responses[match.group(1)] if match else {"schema_version":"aegis.manager.response.v1","kind":"message","message":"safe","proposal":None}
  for typ,payload in [("message.start",{}),("message.delta",{"text":json.dumps(response)}),("message.complete",{"status":"complete"})]:
   emit({"jsonrpc":"2.0","method":"event","params":{"type":typ,"session_id":"fixture","payload":payload}})
`
			if err := os.WriteFile(filepath.Join(root, "python"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			opened := false
			runner.start = func() error {
				// Restart must occur only after the command releases its authority store.
				store, err := authoritybadger.Open(context.Background(), authorityPath)
				if err != nil {
					return fmt.Errorf("restart found command-owned store: %w", err)
				}
				t.Cleanup(func() { _ = store.Close() })
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: cfg.API.UnixSocket, Net: "unix"})
				if err != nil {
					return err
				}
				if err = os.Chmod(cfg.API.UnixSocket, 0600); err != nil {
					return err
				}
				gateway := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					switch {
					case r.URL.Path == "/readyz":
						fmt.Fprint(w, `{"status":"ready","audit":{"state":"current","current":true,"verifiable":true}}`)
					case r.Method == "POST" && r.URL.Path == "/v1/manager/sessions":
						opened = true
						w.WriteHeader(http.StatusCreated)
						_ = json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "token": "fixture-token", "expires": time.Now().Add(time.Hour), "mode": "conversational"})
					case r.Method == "DELETE":
						w.WriteHeader(http.StatusNoContent)
					default:
						t.Errorf("unexpected gateway request %s %s", r.Method, r.URL.Path)
						w.WriteHeader(404)
					}
				})}
				go func() { _ = gateway.Serve(listener) }()
				t.Cleanup(func() { _ = gateway.Close() })
				return nil
			}
			var out bytes.Buffer
			command := NewRoot(Dependencies{Profile: ProductionProfile, In: &bootstrapApprovalInput{out: &out, endpoint: server.URL}, Out: &out, Err: io.Discard, UserService: runner, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
			args := []string{"--config", path}
			if entry == "init" {
				args = append(args, "init")
			}
			command.SetArgs(args)
			if err := command.Execute(); err != nil {
				t.Fatalf("approved resume: %v\n%s", err, out.String())
			}
			for _, want := range []string{"READY / verified", "Canonical built-in Aegis Agent registered=true", "Start authenticated manager"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out.String())
				}
			}
			if !opened || !runner.active || len(runner.runs) != 2 || runner.runs[0][0] != "stop" || runner.runs[1][0] != "start" {
				t.Fatalf("activation/entry missing: opened=%t runs=%v output=%s", opened, runner.runs, out.String())
			}
			current, err := config.Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(current.Manager.Inference.Certification); err != nil {
				t.Fatal(err)
			}
		})
	}
}
