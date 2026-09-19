//go:build linux

package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/api"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
	"github.com/berryhill/aegis/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

func TestGatewayIntakePTY(t *testing.T) {
	for _, scenario := range []string{"typed", "multiline", "decline", "typeahead", "interrupt", "eof", "mismatch"} {
		t.Run(scenario, func(t *testing.T) { testGatewayIntakePTY(t, scenario) })
	}
}

// TestInstalledGatewayIntakePTY executes an independently built release binary.
// Only service-manager observation is a fixture; the CLI, Unix API, principal
// authentication, terminal intake and encrypted persistence are real. This is
// not live-model or systemd deployment acceptance.
func TestInstalledGatewayIntakePTY(t *testing.T) {
	binary := os.Getenv("AEGIS_INTAKE_ACCEPTANCE_BINARY")
	if binary == "" {
		t.Skip("set AEGIS_INTAKE_ACCEPTANCE_BINARY to an exact-candidate installed binary")
	}
	for _, scenario := range []string{"typed", "multiline", "decline", "typeahead", "interrupt", "eof", "mismatch"} {
		t.Run(scenario, func(t *testing.T) { testGatewayIntakePTYBinary(t, scenario, binary) })
	}
}

func testGatewayIntakePTY(t *testing.T, scenario string) {
	testGatewayIntakePTYBinary(t, scenario, "")
}

func testGatewayIntakePTYBinary(t *testing.T, scenario, binary string) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.StateDir = state.Root()
	cfg.Audit.CheckpointDir = state.CheckpointRoot()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Principal = config.Principal{ID: "principal", Name: "Test", UID: strconv.Itoa(os.Getuid()), User: current.Username, AuthTTL: time.Minute}
	material := make([]byte, 32)
	_, _ = rand.Read(material)
	cfg.API.Token = hex.EncodeToString(material)
	cfg.API.UnixSocket = filepath.Join(root, "a.sock")
	cfg.API.Listen = "127.0.0.1:0"
	verifier, err := principalauth.Enroll(cfg.Principal.ID, []byte(cfg.API.Token))
	clear(material)
	if err != nil {
		t.Fatal(err)
	}
	if err = principalauth.Publish(filepath.Join(cfg.StateDir, "auth", principalauth.FileName), verifier); err != nil {
		t.Fatal(err)
	}
	authorityPath := filepath.Join(state.Root(), "persistence", "authority-v1")
	if _, err = authoritybadger.Initialize(context.Background(), authorityPath); err != nil {
		t.Fatal(err)
	}
	authority, err := authoritybadger.Open(context.Background(), authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	key := filepath.Join(root, "key")
	if err = credentials.CreateHostKey(key, "test-key"); err != nil {
		t.Fatal(err)
	}
	custody, err := credentials.LoadFileCustodian(key)
	if err != nil {
		t.Fatal(err)
	}
	defer custody.Close()
	database := filepath.Join(root, "authority.db")
	repo, err := credentialbolt.Open(context.Background(), database, "test", custody)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repo.Close() }()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := app.New(cfg, state, authority, authority, nil, logger)
	service.CredentialAuthority = credentials.NewAuthority(repo, custody)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() { serverDone <- api.Serve(ctx, service) }()
	defer func() {
		cancel()
		select {
		case err := <-serverDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("gateway did not stop")
		}
	}()
	// Readiness is an actual Unix connection, not elapsed time.
	for {
		conn, dialErr := net.DialTimeout("unix", cfg.API.UnixSocket, 20*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		select {
		case err := <-serverDone:
			serverDone <- err
			t.Fatalf("gateway startup: %v", err)
		case <-ctx.Done():
			t.Fatal("gateway readiness deadline")
		case <-time.After(10 * time.Millisecond):
		}
	}
	master, slave := openCommandPTY(t)
	defer master.Close()
	defer slave.Close()
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	cmd.SetErr(slave)
	done := make(chan error, 1)
	if binary == "" {
		client, err := newGatewayManagerClient(cfg)
		if err != nil {
			t.Fatal(err)
		}
		expires, err := client.open(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer client.close(context.Background())
		go func() { done <- runGatewayManagerLoop(cmd, cfg, client, expires) }()
	} else {
		process := installedIntakeCommand(t, ctx, root, binary, cfg, slave)
		if err := process.Start(); err != nil {
			t.Fatal("installed terminal could not start")
		}
		go func() { done <- process.Wait() }()
		t.Cleanup(func() { _ = process.Process.Kill() })
	}
	var capture bytes.Buffer
	wait := func(marker string) {
		deadline := time.Now().Add(5 * time.Second)
		for !bytes.Contains(capture.Bytes(), []byte(marker)) {
			if time.Now().After(deadline) {
				select {
				case loopErr := <-done:
					t.Fatalf("terminal stopped before phase %q: %v", marker, loopErr)
				default:
				}
				t.Fatalf("PTY did not reach expected phase %q (transcript suppressed)", marker)
			}
			poll := []unix.PollFd{{Fd: int32(master.Fd()), Events: unix.POLLIN}}
			ready, e := unix.Poll(poll, 50)
			if e != nil {
				continue
			}
			if ready == 0 {
				continue
			}
			b := make([]byte, 4096)
			n, e := master.Read(b)
			if e != nil {
				t.Fatal("PTY read failed")
			}
			capture.Write(b[:n])
			clear(b)
		}
	}
	send := func(value []byte) {
		if _, err := master.Write(value); err != nil {
			t.Fatal("PTY write failed")
		}
	}
	wait("? help; Ctrl-D exit) ")
	phrase := "can we add a secret for a test?\r"
	if scenario == "multiline" {
		phrase = "can we add a secret as a test\r"
	}
	send([]byte(phrase))
	wait("Non-secret reference: ")
	send([]byte("disposable\n"))
	wait("Non-secret kind")
	if scenario == "typeahead" {
		send([]byte("opaque\nyes\n"))
	} else {
		send([]byte("\n"))
	}
	wait("Type exactly yes")
	if !bytes.Contains(capture.Bytes(), []byte("reference=disposable kind=opaque principal=principal")) {
		t.Fatal("exact metadata review missing before approval")
	}
	beforeApproval, err := repo.List(ctx, "", 100)
	if err != nil || len(beforeApproval) != 0 {
		t.Fatal("credential persisted before explicit approval")
	}
	capture.Reset()
	successful := scenario == "typed" || scenario == "multiline"
	var value []byte
	if scenario == "decline" || scenario == "typeahead" {
		send([]byte("no\n"))
	} else {
		send([]byte("yes\n"))
		wait("Secret value (no echo): ")
		switch scenario {
		case "interrupt":
			send([]byte{3})
		case "eof":
			send([]byte{4})
		default:
			random := make([]byte, 32)
			_, _ = rand.Read(random)
			value = []byte(hex.EncodeToString(random))
			clear(random)
			defer clear(value)
			if scenario == "multiline" {
				value = append(value, '\n')
				value = append(value, []byte("second-line")...)
			}
			wire := append([]byte(nil), value...)
			if scenario == "multiline" {
				wire = append([]byte(protectedPasteStart), wire...)
				wire = append(wire, protectedPasteEnd...)
			}
			wire = append(wire, '\n')
			send(wire)
			wait("Confirm secret value (no echo): ")
			if scenario == "mismatch" {
				send([]byte("different\n"))
			} else {
				send(wire)
			}
			clear(wire)
		}
	}
	if successful {
		wait("Credential created: record=")
	} else {
		wait("Credential creation cancelled")
	}
	wait("? help; Ctrl-D exit) ")
	if len(value) > 0 && bytes.Contains(capture.Bytes(), bytes.SplitN(value, []byte{'\n'}, 2)[0]) {
		t.Fatal("value echoed in protected dialog")
	}
	// Continue a deterministic command in the same authenticated conversation.
	capture.Reset()
	send([]byte("/status\r"))
	wait("manager.status:")
	wait("? help; Ctrl-D exit) ")
	if len(value) > 0 && bytes.Contains(capture.Bytes(), bytes.SplitN(value, []byte{'\n'}, 2)[0]) {
		t.Fatal("value leaked into subsequent conversation")
	}
	send([]byte("/exit\r"))
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("terminal did not return")
	}
	assertCommandPTYRestored(t, slave, initial)
	records, err := repo.List(context.Background(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	expected := 0
	if successful {
		expected = 1
	}
	if len(records) != expected {
		t.Fatalf("records=%d want=%d", len(records), expected)
	}
	if successful {
		if records[0].Reference != "disposable" {
			t.Fatal("metadata mismatch")
		}
		if err = repo.Close(); err != nil {
			t.Fatal(err)
		}
		repo, err = credentialbolt.Open(context.Background(), database, "test", custody)
		if err != nil {
			t.Fatal(err)
		}
		reopened, err := repo.List(context.Background(), "", 100)
		if err != nil || len(reopened) != 1 || reopened[0].ID != records[0].ID {
			t.Fatal("reopen metadata mismatch")
		}
		if err = credentials.NewAuthority(repo, custody).ReadValue(context.Background(), "disposable", func(_ credentials.SecretRecord, decrypted []byte) error {
			if !bytes.Equal(decrypted, value) {
				t.Error("reopened protected bytes differ from confirmed input")
			}
			return nil
		}); err != nil {
			t.Fatal("reopened protected value readback failed")
		}
	}
	// Scan all disposable files, including custody outside state.Root(). Check
	// the generated first line as well as the complete multiline representation.
	if len(value) > 0 {
		canary := bytes.SplitN(value, []byte{'\n'}, 2)[0]
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.Type().IsRegular() {
				return nil
			}
			file, e := os.Open(path)
			if e != nil {
				return e
			}
			defer file.Close()
			found, e := streamContainsCanary(file, canary)
			if e != nil {
				return e
			}
			if found {
				t.Error("protected value retained in application state")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
