//go:build linux

package command

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
	"golang.org/x/sys/unix"
)

func TestAuthorityRecoveryReviewedTransportDenials(t *testing.T) {
	for _, scenario := range []string{"live", "bound", "cancelled", "wrong-password", "config-drift", "verifier-drift", "socket-drift", "context-cancelled", "corrupt-authority", "fleet-refusal", "no-opt-in"} {
		t.Run(scenario, func(t *testing.T) {
			configPath, state := validConfigWithoutOperationalAuthority(t)
			authority := filepath.Join(state, "persistence", "authority-v1")
			if _, err := authoritybadger.Initialize(context.Background(), authority); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(authority, "CLEAN"), filepath.Join(authority, "DIRTY")); err != nil {
				t.Fatal(err)
			}
			dirty, err := os.ReadFile(filepath.Join(authority, "DIRTY"))
			if err != nil {
				t.Fatal(err)
			}
			verifier, err := recoveryPrincipalEnrollment()
			if err != nil {
				t.Fatal(err)
			}
			verifierPath := filepath.Join(state, "auth", principalauth.FileName)
			if err := principalauth.Publish(verifierPath, verifier); err != nil {
				t.Fatal(err)
			}
			dir, err := os.MkdirTemp("", "ar-denial-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			socket := filepath.Join(dir, "s")
			bind := func(keep bool) int {
				fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
				if err != nil {
					t.Fatal(err)
				}
				if err := unix.Bind(fd, &unix.SockaddrUnix{Name: socket}); err != nil {
					unix.Close(fd)
					t.Fatal(err)
				}
				if err := os.Chmod(socket, 0600); err != nil {
					unix.Close(fd)
					t.Fatal(err)
				}
				if !keep {
					unix.Close(fd)
				}
				return fd
			}
			fd := bind(scenario == "bound" || scenario == "live")
			if scenario == "bound" || scenario == "live" {
				defer unix.Close(fd)
			}
			if scenario == "live" {
				if err := unix.Listen(fd, 1); err != nil {
					t.Fatal(err)
				}
			}
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, []byte("api:\n  unix_socket: "+socket+"\n")...)
			if err := os.WriteFile(configPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			provider := &recoveryPasswordFixture{}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "cancelled":
				provider.cancelled = true
			case "wrong-password":
				provider.password = []byte("not-the-enrolled-password")
			case "config-drift":
				provider.before = func() {
					if err := os.WriteFile(configPath, append(data, []byte("# changed during protected input\n")...), 0600); err != nil {
						t.Fatal(err)
					}
				}
			case "verifier-drift":
				provider.before = func() {
					replacement, err := principalauth.Enroll("principal", []byte("different-synthetic-password"))
					if err != nil {
						t.Fatal(err)
					}
					if err = os.Remove(verifierPath); err != nil {
						t.Fatal(err)
					}
					if err = principalauth.Publish(verifierPath, replacement); err != nil {
						t.Fatal(err)
					}
				}
			case "socket-drift":
				provider.before = func() {
					if err := os.Rename(socket, socket+".before"); err != nil {
						t.Fatal(err)
					}
					bind(false)
				}
			case "context-cancelled":
				provider.before = cancel
			case "corrupt-authority":
				if err := os.WriteFile(filepath.Join(authority, "CLEAN"), []byte("not-the-active-generation\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "fleet-refusal":
				if err := os.Mkdir(filepath.Join(state, "persistence", "fleet-v1"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.Lstat(socket)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			root := NewRoot(Dependencies{In: strings.NewReader("yes cannot authenticate\n"), Out: &out, Err: io.Discard, Passphrases: provider})
			root.SetContext(ctx)
			args := []string{"--config", configPath, "authority", "recover-interrupted"}
			if scenario != "no-opt-in" {
				args = append(args, "--quarantine-orphan-transport")
			}
			root.SetArgs(args)
			err = root.Execute()
			if err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if strings.Contains(out.String(), `"recovered": true`) {
				t.Fatalf("failure reported success: %s", out.String())
			}
			if scenario == "live" || scenario == "bound" {
				if provider.acquisitions != 0 {
					t.Fatal("known bound transport requested protected input")
				}
			}
			quarantined, _ := filepath.Glob(socket + ".upgrade-orphan-*")
			if scenario == "corrupt-authority" || scenario == "fleet-refusal" {
				if len(quarantined) != 1 || !strings.Contains(out.String(), `"quarantined_transport"`) {
					t.Fatalf("failed recovery lost custody: %s %v", out.String(), quarantined)
				}
				retained, e := os.Lstat(quarantined[0])
				if e != nil || !os.SameFile(before, retained) {
					t.Fatal("quarantine lost original inode")
				}
				return
			}
			if len(quarantined) != 0 {
				t.Fatal("denial quarantined transport")
			}
			after, e := os.Lstat(socket)
			if e != nil {
				t.Fatal("denial lost transport", e)
			}
			if scenario != "socket-drift" && !os.SameFile(before, after) {
				t.Fatal("denial replaced transport")
			}
			afterDirty, e := os.ReadFile(filepath.Join(authority, "DIRTY"))
			if e != nil || !bytes.Equal(dirty, afterDirty) {
				t.Fatal("denial changed authority")
			}
			if _, e := os.Stat(filepath.Join(authority, "recovery-evidence")); !os.IsNotExist(e) {
				t.Fatal("denial started store recovery")
			}
		})
	}
}
