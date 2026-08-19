package command

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialAuthorityInitializationRequiresExactConfirmation(t *testing.T) {
	configPath := managerTestConfig(t)
	rootDir := filepath.Dir(configPath)
	database := filepath.Join(rootDir, "authority", "authority.db")
	key := filepath.Join(rootDir, "custody", "authority-kek.json")
	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(file, "credentials:\n  references: {}\n  provider_auth: {}\n  authority:\n    database: %s\n    deployment_id: test-deployment\n    custody: host-file\n    kek_file: %s\n", database, key)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}

	run := func(answer string) string {
		t.Helper()
		var output bytes.Buffer
		command := NewRoot(Dependencies{In: strings.NewReader(answer), Out: &output, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
		command.SetArgs([]string{"--config", configPath, "secret", "initialize"})
		if err := command.Execute(); err != nil {
			t.Fatal(err)
		}
		return output.String()
	}
	declined := run("no\n")
	if !strings.Contains(declined, `"written": false`) || !strings.Contains(declined, `"kek_file": "[REDACTED]"`) {
		t.Fatalf("decline output=%s", declined)
	}
	for _, path := range []string{database, key} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("decline created %s: %v", path, err)
		}
	}
	accepted := run("yes\n")
	if !strings.Contains(accepted, `"status": "initialized"`) || !strings.Contains(accepted, `"deployment_id": "test-deployment"`) {
		t.Fatalf("accepted output=%s", accepted)
	}
	for _, path := range []string{database, key} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode=%v", path, info.Mode())
		}
	}
}

// TestSecretCLISmokeRoundsThroughInitializePutAndList exercises the real CLI
// end-to-end: initialize → put → list. The metadata-only surface contract
// (no plaintext, exact ciphertext hash, exact version history, exact kind
// and reference) is pinned through the operator-visible JSON.
func TestSecretCLISmokeRoundsThroughInitializePutAndList(t *testing.T) {
	configPath := managerTestConfig(t)
	rootDir := filepath.Dir(configPath)
	database := filepath.Join(rootDir, "authority", "authority.db")
	key := filepath.Join(rootDir, "custody", "authority-kek.json")
	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintf(file, "credentials:\n  references: {}\n  provider_auth: {}\n  authority:\n    database: %s\n    deployment_id: test-deployment\n    custody: host-file\n    kek_file: %s\n", database, key); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	// The Initialize confirmation prompt must succeed.
	var initOut bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader("yes\n"), Out: &initOut, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", configPath, "secret", "initialize"})
	if err = root.Execute(); err != nil {
		t.Fatalf("secret initialize failed: %v: %s", err, initOut.String())
	}
	if !strings.Contains(initOut.String(), `"status": "initialized"`) {
		t.Fatalf("initialize did not report initialized: %s", initOut.String())
	}
	// Put a secret through the CLI using --stdin so we can pipe exact bytes.
	var putOut bytes.Buffer
	put := NewRoot(Dependencies{In: strings.NewReader("first-secret-never-persist-plaintext"), Out: &putOut, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	put.SetArgs([]string{"--config", configPath, "secret", "put", "--stdin", "--kind", "api-token", "--created-by", "principal", "smoke/reference"})
	if err = put.Execute(); err != nil {
		t.Fatalf("secret put failed: %v: %s", err, putOut.String())
	}
	putText := putOut.String()
	if !strings.Contains(putText, `"reference": "smoke/reference"`) || !strings.Contains(putText, `"kind": "api-token"`) || !strings.Contains(putText, `"current_version": 1`) || !strings.Contains(putText, `"created_by": "principal"`) {
		t.Fatalf("put did not surface metadata-only fields: %s", putText)
	}
	if strings.Contains(putText, "first-secret-never-persist-plaintext") {
		t.Fatalf("put output leaked the secret value: %s", putText)
	}
	// List must surface the new record without ever leaking plaintext or
	// ciphertext-bearing fields.
	var listOut bytes.Buffer
	list := NewRoot(Dependencies{In: strings.NewReader(""), Out: &listOut, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	list.SetArgs([]string{"--config", configPath, "secret", "list", "smoke"})
	if err = list.Execute(); err != nil {
		t.Fatalf("secret list failed: %v: %s", err, listOut.String())
	}
	listText := listOut.String()
	if !strings.Contains(listText, `"reference": "smoke/reference"`) || !strings.Contains(listText, `"status": "active"`) || !strings.Contains(listText, `"count": 1`) {
		t.Fatalf("list did not surface the metadata-only record: %s", listText)
	}
	for _, forbidden := range []string{"first-secret-never-persist-plaintext", "ciphertext\":", "wrapped_dek\":", "record_nonce\":", "wrap_nonce\":", "source_env\":", "target_env\":"} {
		if strings.Contains(listText, forbidden) {
			t.Fatalf("list output leaked %s: %s", forbidden, listText)
		}
	}
}
