//go:build linux

package command

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/api"
	"github.com/berryhill/aegis/internal/reset"
	"github.com/spf13/cobra"
)

func TestServeStartupExcludesStoresDuringRecovery(t *testing.T) {
	configPath, state := validConfigWithoutOperationalAuthority(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "s")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("api:\n  unix_socket: "+socket+"\n")...)
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	build, closeStores := newRootServiceFactory(Dependencies{}, &rootOptions{configFile: configPath})
	defer closeStores()
	err = reset.WithOfflineTransport(context.Background(), socket, func() error { return nil }, func() error {
		cmd := &cobra.Command{Use: "serve"}
		cmd.SetContext(context.Background())
		cmd.SetOut(io.Discard)
		_, err := build(cmd)
		if err == nil || !strings.Contains(err.Error(), "owns this transport") {
			t.Fatalf("startup reached stores: %v", err)
		}
		if _, err := os.Stat(filepath.Join(state, "persistence")); !os.IsNotExist(err) {
			t.Fatalf("startup touched stores: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Releasing offline custody lets startup pass the lock and reach the ordinary
	// missing-authority gate; a failed build must release its startup lease too.
	cmd := &cobra.Command{Use: "serve"}
	cmd.SetContext(context.Background())
	if _, err := build(cmd); err == nil || strings.Contains(err.Error(), "owns this transport") {
		t.Fatalf("startup lock leaked: %v", err)
	}
	if err := closeStores(); err != nil {
		t.Fatal(err)
	}
	_, release, err := api.AcquireStartupLock(context.Background(), socket)
	if err != nil {
		t.Fatal("failed-build cleanup leaked lock", err)
	}
	release()
}
