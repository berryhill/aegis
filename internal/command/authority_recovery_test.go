package command

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
)

type recoveryPasswordFixture struct {
	intent       AuthorityPassphraseIntent
	cancelled    bool
	acquisitions int
}

func (p *recoveryPasswordFixture) Acquire(_ context.Context, r AuthorityPassphraseRequest) ([]byte, error) {
	p.intent = r.Intent
	p.acquisitions++
	if p.cancelled {
		return nil, errors.New("cancelled")
	}
	return []byte("synthetic-recovery-password"), nil
}

func authorityRecoveryCLIUsesProtectedPrincipalAuthenticationOffline(t *testing.T) {
	if isolateCommandFixture(t) {
		return
	}
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "cancelled"}[cancelled], func(t *testing.T) {
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
			root.SetArgs([]string{"--config", configPath, "authority", "recover-interrupted"})
			err = root.Execute()
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
