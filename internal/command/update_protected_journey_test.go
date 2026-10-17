package command

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/berryhill/aegis/internal/config"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Exercise the production retained-journey authentication and recovery methods,
// not a callback that assumes authentication. No terminal emulation is used.
func TestInstalledJourneyProtectedRecovery(t *testing.T) {
	for _, scenario := range []string{"healthy", "interrupted", "mismatched-clean", "socket", "cancelled", "verifier-drift", "config-drift", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			path, state := validConfigWithoutOperationalAuthority(t)
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			token := filepath.Join(filepath.Dir(path), "token")
			if err = os.WriteFile(token, []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"), 0600); err != nil {
				t.Fatal(err)
			}
			socket := filepath.Join(filepath.Dir(path), "gateway.sock")
			b = append(b, []byte(fmt.Sprintf("api:\n  token_file: %q\n  unix_socket: %q\n", token, socket))...)
			if err = os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(state, "persistence", "authority-v1")
			if _, err = authoritybadger.Initialize(context.Background(), root); err != nil {
				t.Fatal(err)
			}
			if scenario == "interrupted" {
				if err = os.Rename(filepath.Join(root, "CLEAN"), filepath.Join(root, "DIRTY")); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "mismatched-clean" {
				if err = os.WriteFile(filepath.Join(root, "CLEAN"), []byte("{}\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "socket" {
				if err = os.WriteFile(socket, []byte("must remain"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			verifier, err := principalauth.Enroll("principal", []byte("synthetic-recovery-password"))
			if err != nil {
				t.Fatal(err)
			}
			verifierPath := filepath.Join(state, "auth", principalauth.FileName)
			if err = principalauth.Publish(verifierPath, verifier); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			plan, err := userservice.Preview(executable, path)
			if err != nil {
				t.Fatal(err)
			}
			provider := &recoveryPasswordFixture{cancelled: scenario == "cancelled"}
			cmd := &cobra.Command{}
			cmd.SetContext(context.WithValue(context.Background(), authorityPassphraseContextKey{}, provider))
			cmd.SetErr(io.Discard)
			journal, err := openUpgradeJournal(plan, state, sha256.Sum256(b), selfupdate.CandidateIdentity{Executable: executable, Version: "test", Source: "source", Digest: "digest"})
			if err != nil {
				t.Fatal(err)
			}
			defer journal.close()
			j := &installedJourney{cmd: cmd, executable: executable, configPath: path, plan: plan, cfg: cfg, configDigest: sha256.Sum256(b), journal: journal}
			defer func() { clear(j.password) }()
			if scenario == "verifier-drift" {
				j.password = []byte("synthetic-recovery-password")
				j.verifier = verifier
				j.verifier.PrincipalID = "different"
				j.expires = time.Now().Add(time.Minute)
			}
			if scenario == "config-drift" {
				if err = os.WriteFile(path, append(b, []byte("# drift\n")...), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "expired" {
				j.password = []byte("expired")
				j.verifier = verifier
				j.expires = time.Now().Add(-time.Second)
			}
			err = j.Recover(cmd.Context())
			wantSuccess := scenario == "healthy" || scenario == "interrupted" || scenario == "expired"
			if (err == nil) != wantSuccess {
				t.Fatalf("scenario=%s err=%v", scenario, err)
			}
			if scenario == "interrupted" && journal.receipt.RecoveryEvidence == "" {
				t.Fatal("missing durable recovery evidence binding")
			}
			if wantSuccess && authoritybadger.Inspect(context.Background(), root).State != authoritybadger.StateReady {
				t.Fatal("authority not closed/qualified")
			}
		})
	}
}
