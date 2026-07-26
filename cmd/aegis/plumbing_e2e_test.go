package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/plumbing"
)

func TestInstalledCLIPlumbingProofAndReadback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses POSIX scripts")
	}
	root := t.TempDir()
	binary := filepath.Join(root, "aegis")
	build := exec.Command("go", "build", "-ldflags=-X=github.com/berryhill/aegis/internal/buildinfo.Version=test", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build installed CLI: %v\n%s", err, output)
	}
	installation := filepath.Join(root, "hermes-install")
	if err := os.MkdirAll(filepath.Join(installation, "venv", "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	hermesExecutable := filepath.Join(root, "hermes")
	hermesScript := "#!/bin/sh\necho 'Hermes Agent v0.18.2'\necho 'Install directory: " + installation + "'\n"
	if err := os.WriteFile(hermesExecutable, []byte(hermesScript), 0700); err != nil {
		t.Fatal(err)
	}
	gateway := `#!/bin/sh
[ "${TEST_PROVIDER_KEY:-}" = "installed-proof-secret" ] || exit 41
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","payload":{}}}'
read create
printf '%s\n' '{"jsonrpc":"2.0","id":"create","result":{"session_id":"installed-proof-session"}}'
read prompt
printf '%s\n' '{"jsonrpc":"2.0","id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.start","payload":{}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.delta","payload":{"delta":"installed proof"}}}'
printf '%s\n' '{"jsonrpc":"2.0","method":"event","params":{"type":"message.complete","payload":{}}}'
while read rest; do :; done
`
	if err := os.WriteFile(filepath.Join(installation, "venv", "bin", "python"), []byte(gateway), 0700); err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "aegis.yaml")
	socketPath := filepath.Join(root, "aegis.sock")
	configData := fmt.Sprintf(`state_dir: %s
runtime_default: hermes
hermes_executable: %s
principal:
  id: principal-1
  name: Principal Operator
  uid: %q
  user: %q
  auth_ttl: 5m
api:
  listen: 127.0.0.1:0
  unix_socket: %s
  token: installed-proof-token
audit:
  checkpoint_dir: %s
credentials:
  references: {}
  provider_auth:
    test: {type: environment, source_env: AEGIS_INSTALLED_PROOF_KEY, target_env: TEST_PROVIDER_KEY}
`, filepath.Join(root, "state"), hermesExecutable, current.Uid, current.Username, socketPath, filepath.Join(root, "checkpoints"))
	if err := os.WriteFile(configPath, []byte(configData), 0600); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(root, "prompt.txt")
	expectedPath := filepath.Join(root, "expected.txt")
	if err := os.WriteFile(promptPath, []byte("produce installed proof"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(expectedPath, []byte("installed proof"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(arguments ...string) ([]byte, error) {
		command := exec.Command(binary, append([]string{"--config", configPath}, arguments...)...)
		command.Env = append(os.Environ(), "HOME="+filepath.Join(root, "home"), "AEGIS_INSTALLED_PROOF_KEY=installed-proof-secret")
		return command.CombinedOutput()
	}
	if output, runErr := run("plumbing", "poc", "--prompt-file", promptPath, "--expect-file", expectedPath, "--provider", "test", "--model", "fixture"); runErr == nil || !strings.Contains(string(output), "acknowledge-plumbing-unrestricted") {
		t.Fatalf("missing acknowledgement did not deny before proof: err=%v output=%s", runErr, output)
	}
	output, err := run("plumbing", "poc", "--prompt-file", promptPath, "--expect-file", expectedPath, "--provider", "test", "--model", "fixture", "--acknowledge-plumbing-unrestricted")
	if err != nil {
		t.Fatalf("installed plumbing proof: %v\n%s", err, output)
	}
	var created plumbing.Aggregate
	if err := json.Unmarshal(output, &created); err != nil {
		t.Fatalf("decode created graph run: %v\n%s", err, output)
	}
	if created.Disposition == nil || created.Disposition.State != plumbing.DispositionSucceeded || len(created.Artifacts) != 1 || len(created.Evidence) != 1 || created.Evidence[0].Outcome != plumbing.VerificationPassed {
		t.Fatalf("created graph run lacks verified terminal chain: %#v", created)
	}
	output, err = run("plumbing", "show", created.ID)
	if err != nil {
		t.Fatalf("installed graph readback: %v\n%s", err, output)
	}
	var readback plumbing.Aggregate
	if err := json.Unmarshal(output, &readback); err != nil {
		t.Fatal(err)
	}
	if readback.ID != created.ID || readback.Revision != created.Revision || readback.Disposition.ID != created.Disposition.ID || readback.Authority.Digest != created.Authority.Digest {
		t.Fatalf("readback does not reconstruct the created causal chain: created=%#v readback=%#v", created, readback)
	}
	server := exec.Command(binary, "--config", configPath, "serve")
	server.Env = append(os.Environ(), "HOME="+filepath.Join(root, "home"), "AEGIS_INSTALLED_PROOF_KEY=installed-proof-secret")
	serverOutput := &strings.Builder{}
	server.Stdout, server.Stderr = serverOutput, serverOutput
	if err = server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if server.Process != nil {
			_ = server.Process.Signal(os.Interrupt)
			_, _ = server.Process.Wait()
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		connection, dialErr := net.DialTimeout("unix", socketPath, 50*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("installed API did not open Unix socket: %v\n%s", dialErr, serverOutput.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}, Timeout: 5 * time.Second}
	request, err := http.NewRequest(http.MethodGet, "http://unix/v1/graph-runs/"+created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer installed-proof-token")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	responseBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("installed API graph readback status=%d body=%s", response.StatusCode, responseBody)
	}
	var apiReadback plumbing.Aggregate
	if err = json.Unmarshal(responseBody, &apiReadback); err != nil {
		t.Fatal(err)
	}
	if apiReadback.ID != created.ID || apiReadback.Disposition.ID != created.Disposition.ID || apiReadback.Authority.Digest != created.Authority.Digest {
		t.Fatalf("API readback does not reconstruct the created causal chain: %#v", apiReadback)
	}
	entries, err := os.ReadDir(filepath.Join(root, "state", "runtime"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("disposable Hermes homes were retained: %v", entries)
	}
}
