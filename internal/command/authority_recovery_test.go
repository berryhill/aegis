package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
)

type recoveryPasswordFixture struct {
	intent       AuthorityPassphraseIntent
	cancelled    bool
	acquisitions int
	before       func()
	password     []byte
}

func (p *recoveryPasswordFixture) Acquire(_ context.Context, r AuthorityPassphraseRequest) ([]byte, error) {
	p.intent = r.Intent
	p.acquisitions++
	if p.before != nil {
		p.before()
	}
	if p.cancelled {
		return nil, errors.New("cancelled")
	}
	if p.password != nil {
		return append([]byte(nil), p.password...), nil
	}
	return []byte("synthetic-recovery-password"), nil
}

func authorityRecoveryCLIUsesProtectedPrincipalAuthenticationOffline(t *testing.T) {
	if isolateCommandFixture(t) {
		return
	}
	for _, scenario := range []string{"success", "cancelled", "orphan"} {
		t.Run(scenario, func(t *testing.T) {
			cancelled := scenario == "cancelled"
			if scenario == "orphan" && runtime.GOOS != "linux" {
				t.Skip("kernel-proven orphan quarantine is Linux-only")
			}
			configPath, state := validConfigWithoutOperationalAuthority(t)
			path := filepath.Join(state, "persistence", "authority-v1")
			g, err := authoritybadger.Initialize(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(filepath.Join(path, "CLEAN"), filepath.Join(path, "DIRTY")); err != nil {
				t.Fatal(err)
			}
			verifier, err := recoveryPrincipalEnrollment()
			if err != nil {
				t.Fatal(err)
			}
			if err = principalauth.Publish(filepath.Join(state, "auth", principalauth.FileName), verifier); err != nil {
				t.Fatal(err)
			}
			provider := &recoveryPasswordFixture{cancelled: cancelled}
			var output bytes.Buffer
			root := NewRoot(Dependencies{In: strings.NewReader("yes must not authenticate\n"), Out: &output, Err: io.Discard, Passphrases: provider})
			args := []string{"--config", configPath, "authority", "recover-interrupted"}
			var orphan os.FileInfo
			var socket string
			if scenario == "orphan" {
				dir, e := os.MkdirTemp("", "ar-")
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { os.RemoveAll(dir) })
				socket = filepath.Join(dir, "s")
				listener, e := net.Listen("unix", socket)
				if e != nil {
					t.Fatal(e)
				}
				listener.(*net.UnixListener).SetUnlinkOnClose(false)
				if e = os.Chmod(socket, 0600); e != nil {
					t.Fatal(e)
				}
				if e = listener.Close(); e != nil {
					t.Fatal(e)
				}
				orphan, e = os.Lstat(socket)
				if e != nil {
					t.Fatal(e)
				}
				data, e := os.ReadFile(configPath)
				if e != nil {
					t.Fatal(e)
				}
				data = append(data, []byte("api:\n  unix_socket: "+socket+"\n")...)
				if e = os.WriteFile(configPath, data, 0600); e != nil {
					t.Fatal(e)
				}
				args = append(args, "--quarantine-orphan-transport")
			}
			root.SetArgs(args)
			err = root.Execute()
			if scenario == "orphan" {
				if err != nil {
					t.Fatalf("authenticated orphan recovery refused: %v", err)
				}
				if _, e := os.Lstat(socket); !os.IsNotExist(e) {
					t.Fatalf("transport remains: %v", e)
				}
				matches, e := filepath.Glob(socket + ".upgrade-orphan-*")
				if e != nil || len(matches) != 1 {
					t.Fatalf("missing retained transport: %v %v", matches, e)
				}
				retained, e := os.Lstat(matches[0])
				if e != nil || !os.SameFile(orphan, retained) {
					t.Fatalf("original socket not preserved: %v", e)
				}
			}
			if provider.intent != PrincipalPasswordAuthenticate {
				t.Fatal("wrong authentication intent")
			}
			if cancelled {
				if err == nil {
					t.Fatal("cancel accepted")
				}
				if _, err = os.Stat(filepath.Join(path, "CLEAN")); !os.IsNotExist(err) {
					t.Fatal("mutated on cancellation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			inspection := authoritybadger.Inspect(context.Background(), path)
			if inspection.State != authoritybadger.StateReady || inspection.Generation != g {
				t.Fatal(inspection)
			}
			if !strings.Contains(output.String(), `"recovered": true`) || !strings.Contains(output.String(), `"evidence_path"`) {
				t.Fatal(output.String())
			}
			for _, name := range []string{"fleet-v1"} {
				if _, err = os.Stat(filepath.Join(state, "persistence", name)); !os.IsNotExist(err) {
					t.Fatal("opened unrelated store")
				}
			}
		})
	}
}
