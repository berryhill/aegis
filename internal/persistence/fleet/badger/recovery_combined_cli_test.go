package badger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
)

// An actual writer exits abruptly with fleet DIRTY and authority normally closed.
// The larger existing delivery fixture supplies canonical histories, not raw
// success records. Only the explicitly malformed negative uses raw corruption.
func TestCombinedRecoveryCLIWriterChild(t *testing.T) {
	root := os.Getenv("AEGIS_COMBINED_RECOVERY_CHILD")
	if root == "" {
		return
	}
	syscall.Umask(0077)
	ctx := context.Background()
	authority := filepath.Join(filepath.Dir(root), "authority-v1")
	if _, err := authoritybadger.Initialize(ctx, authority); err != nil {
		t.Fatal(err)
	}
	a, err := authoritybadger.Open(ctx, authority)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AEGIS_COMBINED_RECOVERY_MODE") != "completed" {
		if err = a.Close(); err != nil {
			t.Fatal(err)
		}
	}
	s, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	_, c, _, _ := recoveryDeliveryFixtureStore(t, s, true)
	entries := []struct{ Key, Value []byte }{}
	for k, v := range recoveryInventory(t, s) {
		entries = append(entries, struct{ Key, Value []byte }{[]byte(k), v})
	}
	wire, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(root), "fleet-before.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AEGIS_COMBINED_RECOVERY_MODE") == "malformed-fleet" {
		setRawRecord(t, s, key(familyContributionPublication, "released", c.OwnerID, c.Key), []byte("{}"))
	}
	if err = s.db.Sync(); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

// Opt-in because this acceptance runs the built public executable, not NewRoot
// with an injected authentication service. It never launches a listener.
func TestCombinedRecoveryPublicCLI(t *testing.T) {
	binary := os.Getenv("AEGIS_COMBINED_RECOVERY_BINARY")
	if binary == "" {
		t.Skip("set AEGIS_COMBINED_RECOVERY_BINARY to the built candidate")
	}
	for _, scenario := range []string{"clean", "completed", "wrong-password", "cancelled", "malformed-authority", "malformed-fleet"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			private := t.TempDir()
			must(os.Chmod(private, 0700))
			state := filepath.Join(private, "state")
			parent := filepath.Join(state, "persistence")
			must(os.MkdirAll(parent, 0700))
			root := filepath.Join(parent, "fleet-v1")
			authority := filepath.Join(parent, "authority-v1")
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCombinedRecoveryCLIWriterChild$")
			child.Env = append(os.Environ(), "TMPDIR="+private, "AEGIS_COMBINED_RECOVERY_CHILD="+root, "AEGIS_COMBINED_RECOVERY_MODE="+scenario)
			if b, err := child.CombinedOutput(); err != nil {
				t.Fatalf("writer: %v %s", err, b)
			}
			var prior []byte
			if scenario == "completed" {
				out, err := authoritybadger.RecoverInterruptedAs(ctx, authority, "fixture-owner")
				must(err)
				if !out.Recovered {
					t.Fatal("authority recovery did not complete")
				}
				prior, err = os.ReadFile(filepath.Join(authority, "recovery-evidence", "receipt.json"))
				must(err)
			}
			if scenario == "malformed-authority" {
				must(os.Mkdir(filepath.Join(authority, "staging", "ambiguous"), 0700))
			}
			clean, err := os.ReadFile(filepath.Join(authority, "CLEAN"))
			must(err)
			if s, err := Open(ctx, root); err == nil {
				s.Close()
				t.Fatal("dirty fleet opened normally")
			}
			verifier, err := principalauth.Enroll("fixture-owner", []byte("synthetic-combined-recovery-password"))
			must(err)
			must(principalauth.Publish(filepath.Join(state, "auth", principalauth.FileName), verifier))
			current, err := user.Current()
			must(err)
			config := filepath.Join(private, "aegis.yaml")
			must(os.WriteFile(config, fmt.Appendf(nil, "state_dir: %q\nprincipal:\n  id: fixture-owner\n  name: Fixture\n  uid: %q\n  user: %q\n  auth_ttl: 2m\naudit:\n  checkpoint_dir: %q\n", state, current.Uid, current.Username, filepath.Join(state, "audit-checkpoints")), 0600))
			pin := filepath.Join(private, "pinentry")
			response := "printf 'D synthetic-combined-recovery-password\\nOK\\n'"
			if scenario == "wrong-password" {
				response = "printf 'D incorrect-combined-recovery-password\\nOK\\n'"
			}
			if scenario == "cancelled" {
				response = "printf 'ERR 83886179 cancelled\\n'"
			}
			must(os.WriteFile(pin, []byte("#!/bin/sh\nprintf 'OK fixture\\n'\nwhile IFS= read -r line; do\ncase \"$line\" in\nGETPIN) "+response+";;\nBYE) printf 'OK\\n'; exit 0;;\n*) printf 'OK\\n';;\nesac\ndone\n"), 0700))
			run := func(executable string) ([]byte, error) {
				cmd := exec.CommandContext(ctx, executable, "--config", config, "--pinentry-executable", pin, "authority", "recover-interrupted")
				cmd.Env = []string{"HOME=" + current.HomeDir, "PATH=/usr/bin:/bin", "TMPDIR=" + private, "LANG=C"}
				return cmd.CombinedOutput()
			}
			if baseline := os.Getenv("AEGIS_COMBINED_RECOVERY_BASELINE"); baseline != "" && scenario == "clean" {
				b, err := run(baseline)
				if err == nil || !strings.Contains(string(b), "unexpected root entry") {
					t.Fatalf("baseline did not reproduce: %v %s", err, b)
				}
				t.Log("baseline public command reproduced unexpected root entry")
			}
			b, err := run(binary)
			positive := scenario == "clean" || scenario == "completed"
			if !positive {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || ctx.Err() != nil {
					t.Fatalf("command did not produce a product denial: %v %s", err, b)
				}
				if err == nil {
					t.Fatalf("invalid state admitted: %s", b)
				}
				if scenario == "wrong-password" && !strings.Contains(string(b), "authentication") {
					t.Fatalf("wrong denial: %s", b)
				}
				if scenario == "cancelled" && !strings.Contains(string(b), "cancel") {
					t.Fatalf("wrong denial: %s", b)
				}
				if _, e := os.Lstat(filepath.Join(root, "CLEAN")); !os.IsNotExist(e) {
					t.Fatal("denial published fleet CLEAN", e)
				}
				if s, e := Open(ctx, root); e == nil {
					s.Close()
					t.Fatal("denied fleet opened normally")
				}
				if scenario == "malformed-authority" && !strings.Contains(string(b), "closed, clean, unambiguous generation") {
					t.Fatalf("wrong authority denial: %s", b)
				}
				if scenario == "malformed-fleet" {
					if _, e := os.Lstat(filepath.Join(root, "recovery-evidence", "receipt.json")); e != nil {
						t.Fatalf("did not reach fleet validation: %v %s", e, b)
					}
				}
				if scenario != "malformed-fleet" {
					if _, e := os.Lstat(filepath.Join(root, "recovery-evidence")); !os.IsNotExist(e) {
						t.Fatal("denial reached fleet mutation", e)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("public recovery: %v %s", err, b)
				}
				if scenario == "clean" && !strings.Contains(string(b), `"verified_clean": true`) {
					t.Fatalf("missing clean verification result: %s", b)
				}
				first := append([]byte(nil), b...)
				b, err = run(binary)
				if err != nil || !bytes.Equal(first, b) {
					t.Fatalf("public retry changed result: %v %s", err, b)
				}
				s, e := Open(ctx, root)
				must(e)
				wire, e := os.ReadFile(filepath.Join(parent, "fleet-before.json"))
				must(e)
				var entries []struct{ Key, Value []byte }
				must(json.Unmarshal(wire, &entries))
				want := map[string][]byte{}
				for _, entry := range entries {
					want[string(entry.Key)] = entry.Value
				}
				if !reflect.DeepEqual(want, recoveryInventory(t, s)) {
					t.Fatal("logical bytes changed")
				}
				checkPresentationRecovery(t, s, true)
				must(s.Close())
				t.Logf("public recovery/retry and reopened logical byte equality: %d fleet records", len(want))
			}
			got, e := os.ReadFile(filepath.Join(authority, "CLEAN"))
			must(e)
			if !bytes.Equal(clean, got) {
				t.Fatal("authority CLEAN changed")
			}
			if prior != nil {
				got, e = os.ReadFile(filepath.Join(authority, "recovery-evidence", "receipt.json"))
				must(e)
				if !bytes.Equal(prior, got) {
					t.Fatal("prior authority receipt changed")
				}
			} else if _, e := os.Lstat(filepath.Join(authority, "recovery-evidence")); !os.IsNotExist(e) {
				t.Fatal("clean authority evidence fabricated", e)
			}
		})
	}
}
