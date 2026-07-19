package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagedOllamaFakeReadinessAndCleanup(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ollama")
	script := `#!/bin/sh
exec python3 -c 'import json,os
from http.server import BaseHTTPRequestHandler,HTTPServer
host,port=os.environ["OLLAMA_HOST"].rsplit(":",1)
class H(BaseHTTPRequestHandler):
 def do_GET(self):
  if self.path=="/api/version":
   self.send_response(200);self.send_header("Content-Type","application/json");self.end_headers();self.wfile.write(json.dumps({"version":"0.32.0"}).encode())
  else:self.send_response(404);self.end_headers()
 def log_message(self,*args):pass
HTTPServer((host,int(port)),H).serve_forever()'
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	managed, err := StartManagedOllama(context.Background(), executable, filepath.Join(dir, "state"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	home := managed.home
	client, err := NewOllamaClient(managed.Endpoint(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if version, err := client.Version(context.Background()); err != nil || version != "0.32.0" {
		t.Fatalf("version=%q err=%v", version, err)
	}
	concurrentClose(t, func() error { return managed.Close(context.Background()) })
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("managed state retained: %v", err)
	}
	if err := managed.Close(context.Background()); err != nil {
		t.Fatal("idempotent close failed", err)
	}
}

func TestFakeHermesProcessMultiTurnAndDisposableHome(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-hermes-python")
	response := `{"schema_version":"aegis.manager.response.v1","kind":"message","message":"ok","proposal":null}`
	script := `#!/bin/sh
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
while IFS= read -r line; do
 case "$line" in
  *session.create*) printf '%s\n' '{"jsonrpc":"2.0","id":"aegis-1","result":{"session_id":"fake-session"}}' ;;
  *prompt.submit*)
   printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","session_id":"fake-session"}}'
   printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","session_id":"fake-session","payload":{"text":"` + strings.ReplaceAll(response, `"`, `\"`) + `"}}}'
   printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","session_id":"fake-session","payload":{"status":"complete"}}}' ;;
 esac
done
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	process, err := StartHermesProcess(context.Background(), HermesProcessConfig{Python: executable, Installation: dir, StateRoot: filepath.Join(dir, "state"), ProxyEndpoint: "http://127.0.0.1:1", ProxyToken: "capability", Model: "exact:1", MaximumMessageBytes: 1 << 20, StartTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	environment := strings.Join(process.command.Env, "\n")
	for _, required := range []string{"HERMES_SAFE_MODE=1", "HERMES_IGNORE_USER_CONFIG=1", "HERMES_IGNORE_RULES=1", "HERMES_HOME=", "HERMES_TUI_PROVIDER=custom", "OPENAI_BASE_URL=http://127.0.0.1:1/v1", "OPENAI_API_KEY=capability"} {
		if !strings.Contains(environment, required) {
			t.Fatalf("missing Hermes isolation environment %q", required)
		}
	}
	home := process.home
	sessionID, err := process.Client().CreateSession(context.Background(), "aegis-manager")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		output, err := process.Client().Turn(context.Background(), sessionID, "hello", 4096)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = DecodeResponse(output, 4096); err != nil {
			t.Fatal(err)
		}
	}
	concurrentClose(t, func() error { return process.Close(context.Background()) })
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("Hermes home retained: %v", err)
	}
	if err := process.Close(context.Background()); err != nil {
		t.Fatal("idempotent close failed", err)
	}
}

func concurrentClose(t *testing.T, closeComponent func() error) {
	t.Helper()
	start := make(chan struct{})
	errorsFound := make(chan error, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errorsFound <- closeComponent()
		}()
	}
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestHermesCleanupDeadlineForcesProcessGroupAndRemovesHome(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "stubborn-hermes-python")
	script := `#!/bin/sh
trap '' TERM
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
while :; do sleep 1; done
`
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	process, err := StartHermesProcess(context.Background(), HermesProcessConfig{Python: executable, Installation: dir, StateRoot: filepath.Join(dir, "state"), ProxyEndpoint: "http://127.0.0.1:1", ProxyToken: "fixture-capability", Model: "exact:1", MaximumMessageBytes: 1 << 20, StartTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	home := process.home
	cleanup, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = process.Close(cleanup)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup error=%v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("cleanup exceeded bound: %s", time.Since(started))
	}
	if _, err = os.Lstat(home); !os.IsNotExist(err) {
		t.Fatalf("Hermes home retained after forced cleanup: %v", err)
	}
}
