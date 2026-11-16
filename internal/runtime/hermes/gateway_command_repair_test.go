package hermes

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestCommandOutputEveryCopyPathBounded(t *testing.T) {
	for _, path := range []string{"write", "reader-from", "writer-to", "chunks"} {
		t.Run(path, func(t *testing.T) {
			var out commandOutput
			payload := strings.Repeat("x", 20000)
			switch path {
			case "write":
				_, _ = out.Write([]byte(payload))
			case "reader-from":
				_, _ = io.Copy(&out, io.LimitReader(strings.NewReader(payload), int64(len(payload))))
			case "writer-to":
				_, _ = io.Copy(&out, strings.NewReader(payload))
			case "chunks":
				_, _ = out.Write([]byte(payload[:16000]))
				_, _ = out.Write([]byte(payload[16000:]))
				_, _ = out.Write([]byte("discard after overflow"))
			}
			if out.Len() > 16<<10 || !out.overflow {
				t.Fatalf("retained=%d overflow=%v", out.Len(), out.overflow)
			}
		})
	}
	var exact commandOutput
	_, _ = io.Copy(&exact, strings.NewReader(strings.Repeat("x", 16<<10)))
	if exact.Len() != 16<<10 || exact.overflow {
		t.Fatal("exact budget rejected")
	}
	_, _ = exact.Write([]byte("x"))
	if !exact.overflow || exact.Len() != 16<<10 {
		t.Fatal("one byte overflow accepted")
	}
}

func TestModernCommandRealChildValidOversizedRecord(t *testing.T) {
	d, home, python := commandFixture(t, nil)
	record, _ := json.Marshal([]string{python, "-I", "-c", gatewayBootstrap(d.Installation)})
	literal, _ := json.Marshal(string(record))
	script := "import sys; print(" + string(literal) + "); sys.stdout.write(' '*20000)"
	launcher := "#!/bin/sh\nexec " + python + " -I -c '" + strings.ReplaceAll(script, "'", "'\"'\"'") + "' \"$@\"\n"
	if err := os.WriteFile(d.Executable, []byte(launcher), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := discoverGatewayCommand(context.Background(), d, home); !errors.Is(err, errGatewayCommand) {
		t.Fatal("valid oversized record accepted", err)
	}
}

func TestModernActivationFreshnessIdentity(t *testing.T) {
	// Explicit independent inventory, not a loop over the implementation's list.
	for _, name := range []string{"setup.py", "pm/lock.json", "hermes_cli/venv_sync.py", "hermes_cli/runtime_state.py", "pm/__init__.py", "pm/paths.py", "pm/filesystem.py"} {
		for _, change := range []string{"bytes", "inode", "path", "absent", "missing-at-discovery"} {
			t.Run(name+"/"+change, func(t *testing.T) {
				d, home, _ := commandFixture(t, nil)
				path := filepath.Join(d.Installation, name)
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if change == "missing-at-discovery" {
					if err = os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if _, err = discoverGatewayCommand(context.Background(), d, home); !errors.Is(err, errGatewayCommand) {
						t.Fatal("missing input accepted", err)
					}
					return
				}
				g, err := discoverGatewayCommand(context.Background(), d, home)
				if err != nil {
					t.Fatal(err)
				}
				switch change {
				case "bytes":
					changed := append([]byte(nil), data...)
					changed[0] ^= 1
					if err = os.WriteFile(path, changed, info.Mode()); err != nil {
						t.Fatal(err)
					}
					if err = os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
						t.Fatal(err)
					}
				case "inode":
					if err = os.WriteFile(path+".new", data, info.Mode()); err != nil {
						t.Fatal(err)
					}
					if err = os.Chtimes(path+".new", info.ModTime(), info.ModTime()); err != nil {
						t.Fatal(err)
					}
					if err = os.Rename(path+".new", path); err != nil {
						t.Fatal(err)
					}
				case "path":
					// Same target inode/bytes, but a different pathname resolution.
					if err = os.Rename(path, path+".moved"); err != nil {
						t.Fatal(err)
					}
					if err = os.Symlink(path+".moved", path); err != nil {
						t.Fatal(err)
					}
				case "absent":
					if err = os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				if !errors.Is(g.fresh(), errGatewayCommand) {
					t.Fatal("input drift accepted")
				}
			})
		}
	}
}

func TestModernInvalidStartupHasNoCredentialArtifact(t *testing.T) {
	for _, mode := range []string{"missing", "malformed", "cancel-during-query"} {
		t.Run(mode, func(t *testing.T) {
			d, _, python := commandFixture(t, nil)
			a, root := attemptTestAdapter(t, "#!/bin/sh\nexit 91\n")
			version := "#!/bin/sh\necho 'Hermes Agent v0.23.0'\necho 'Install directory: " + d.Installation + "'\n"
			if err := os.WriteFile(filepath.Join(root, "hermes"), []byte(version), 0700); err != nil {
				t.Fatal(err)
			}
			a.providerTransportQualifier = func(core.RuntimeDescriptor) error { return nil }
			calls := 0
			a.SetProviderAuthenticationResolver(func(context.Context, core.HermesConfig) (ProviderAccessToken, error) {
				calls++
				return ProviderAccessToken{}, ErrProviderAuthAbsent
			})
			request := providerAttempt(root)
			request.Launch.Mandate.Runtime.Version = "0.23.0"
			request.Launch.AuthorityContext.Runtime.Version = "0.23.0"
			request.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(request.Launch.AuthorityContext)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			marker := filepath.Join(root, "query-clean")
			if mode == "missing" {
				if err := os.Remove(d.Executable); err != nil {
					t.Fatal(err)
				}
			} else {
				// Observe both the empty query home and its credential-free attempt parent
				// while the child is running, not just after cleanup removed evidence.
				script := "import os, pathlib, time; h=pathlib.Path(os.environ['HOME']); assert not list(h.iterdir()); assert not (h.parent/'auth.json').exists(); pathlib.Path(" + string(mustJSON(t, marker)) + ").write_text('clean'); "
				if mode == "malformed" {
					script += "print('{}')"
				} else {
					script += "time.sleep(20)"
				}
				launcher := "#!/bin/sh\nexec " + python + " -I -c '" + strings.ReplaceAll(script, "'", "'\"'\"'") + "' \"$@\"\n"
				if err := os.WriteFile(d.Executable, []byte(launcher), 0700); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan error, 1)
			go func() { _, err := a.AttemptTurn(ctx, request); done <- err }()
			if mode == "cancel-during-query" {
				deadline := time.Now().Add(3 * time.Second)
				for {
					if b, err := os.ReadFile(marker); err == nil && string(b) == "clean" {
						break
					}
					if time.Now().After(deadline) {
						cancel()
						<-done
						t.Fatal("query never observed")
					}
					time.Sleep(time.Millisecond)
				}
				cancel()
			}
			err := <-done
			if err == nil || calls != 0 {
				t.Fatalf("err=%v resolver calls=%d", err, calls)
			}
			if mode != "missing" {
				if b, e := os.ReadFile(marker); e != nil || string(b) != "clean" {
					t.Fatal("query saw credential state", e)
				}
			}
			entries, e := os.ReadDir(filepath.Join(root, "state/runtime"))
			if e != nil || len(entries) != 0 {
				t.Fatal("startup left auth/home", e)
			}
		})
	}
}

func mustJSON(t *testing.T, v string) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
