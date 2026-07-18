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
