package initialize

import (
	"bytes"
	"context"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
)

func TestRecoverInterruptedAuthorityAuthenticatedOfflineBoundary(t *testing.T) {
	for _, scenario := range []string{"valid", "wrong-password", "wrong-os-principal", "wrong-verifier-principal", "missing-verifier", "online"} {
		t.Run(scenario, func(t *testing.T) {
			state := filepath.Join(t.TempDir(), "state")
			if err := os.Mkdir(state, 0700); err != nil {
				t.Fatal(err)
			}
			cfg := config.Defaults().WithStateDir(state)
			current, err := user.Current()
			if err != nil {
				t.Fatal(err)
			}
			cfg.Principal = config.Principal{ID: "recovery-operator", Name: "Recovery operator", UID: current.Uid, User: current.Username, AuthTTL: time.Minute}
			root := filepath.Join(state, "persistence", "authority-v1")
			g, err := authoritybadger.Initialize(context.Background(), root)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(filepath.Join(root, "CLEAN"), filepath.Join(root, "DIRTY")); err != nil {
				t.Fatal(err)
			}
			principal := cfg.Principal.ID
			if scenario == "wrong-verifier-principal" {
				principal = "other-operator"
			}
			verifier, err := principalauth.Enroll(principal, []byte("synthetic-recovery-password"))
			if err != nil {
				t.Fatal(err)
			}
			if scenario != "missing-verifier" {
				if err = principalauth.Publish(filepath.Join(state, "auth", principalauth.FileName), verifier); err != nil {
					t.Fatal(err)
				}
			}
			password := []byte("synthetic-recovery-password")
			if scenario == "wrong-password" {
				password = []byte("incorrect-recovery-password")
			}
			if scenario == "wrong-os-principal" {
				cfg.Principal.UID = "not-the-current-uid"
			}
			if scenario == "online" {
				cfg.API.UnixSocket = filepath.Join(state, "occupied.sock")
				if err = os.WriteFile(cfg.API.UnixSocket, []byte("occupied"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			dirty, _ := os.ReadFile(filepath.Join(root, "DIRTY"))
			result, err := RecoverInterruptedAuthority(context.Background(), cfg, password)
			if scenario == "valid" {
				if err != nil || !result.Recovered || result.Generation != g {
					t.Fatalf("%+v %v", result, err)
				}
			} else {
				if err == nil || result.Recovered {
					t.Fatal("unauthenticated recovery accepted")
				}
				after, _ := os.ReadFile(filepath.Join(root, "DIRTY"))
				if !bytes.Equal(dirty, after) {
					t.Fatal("denial changed marker")
				}
				if _, err = os.Stat(filepath.Join(root, "recovery-evidence")); !os.IsNotExist(err) {
					t.Fatal("denial mutated authority")
				}
			}
			if !bytes.Equal(password, make([]byte, len(password))) {
				t.Fatal("password not cleared")
			}
		})
	}
}
