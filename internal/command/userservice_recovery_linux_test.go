package command

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/principalauth"
	"github.com/berryhill/aegis/internal/userservice"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recoveryCLIRunner struct{ writes int }

func (r *recoveryCLIRunner) Run(context.Context, ...string) error { r.writes++; return nil }
func (r *recoveryCLIRunner) Output(_ context.Context, a ...string) ([]byte, error) {
	if len(a) > 3 {
		switch a[3] {
		case "MainPID":
			return []byte("0"), nil
		case "ActiveState":
			return []byte("inactive"), nil
		case "DropInPaths":
			return nil, nil
		}
	}
	return nil, nil
}
func recoveryCLIApprovalRejections(t *testing.T) {
	if isolateCommandFixture(t) {
		return
	}
	// Build release metadata locally; approval rejection coverage needs no
	// downloaded historical release or operator-owned proof artifacts.
	old := mustCommandRead(t, upgradeReleaseBuild(t, t.TempDir(), "0.2.29"))
	for _, scenario := range []string{"noninteractive", "missing-auth", "wrong-password", "confirmation"} {
		t.Run(scenario, func(t *testing.T) {
			original, state := validConfigWithoutOperationalAuthority(t)
			home := filepath.Dir(original)
			os.Chmod(home, 0700)
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
			cfg := filepath.Join(home, ".aegis", "aegis.yaml")
			canonicalState := filepath.Join(home, ".aegis", "state")
			data := mustCommandRead(t, original)
			data = []byte(strings.ReplaceAll(string(data), state, canonicalState))
			data = append(data, []byte("api:\n  token_file: "+filepath.Join(canonicalState, "transport", "api.token")+"\n  unix_socket: "+filepath.Join(canonicalState, "transport", "a.sock")+"\n")...)
			upgradeWrite(t, filepath.Join(canonicalState, "transport", "api.token"), []byte(strings.Repeat("a", 64)), 0600)
			upgradeWrite(t, cfg, data, 0600)
			exe := filepath.Join(home, "bin", "aegis")
			upgradeWrite(t, exe, old, 0700)
			plan, e := userservice.Preview(exe, cfg)
			if e != nil {
				t.Fatal(e)
			}
			foreign := []byte("foreign recovery CLI fixture")
			upgradeWrite(t, plan.UnitPath, foreign, 0600)
			if scenario != "missing-auth" {
				v, e := recoveryPrincipalEnrollment()
				if e != nil {
					t.Fatal(e)
				}
				if e = principalauth.Publish(filepath.Join(canonicalState, "auth", principalauth.FileName), v); e != nil {
					t.Fatal(e)
				}
			}
			password := []byte("synthetic-recovery-password")
			if scenario == "wrong-password" {
				password = []byte("synthetic-wrong-password")
			}
			runner := &recoveryCLIRunner{}
			root := NewRoot(Dependencies{Profile: ProductionProfile, In: strings.NewReader("no\n"), Out: io.Discard, Err: io.Discard, UserService: runner, Passphrases: &recordingPassphrases{value: password}, IsTerminal: func(io.Reader, io.Writer) bool { return scenario != "noninteractive" }})
			root.SetArgs([]string{"--config", cfg, "gateway", "recover", "--installed-executable", exe, "--apply"})
			e = root.Execute()
			if e == nil {
				t.Fatal("approval rejection accepted")
			}
			switch scenario {
			case "noninteractive":
				if !strings.Contains(e.Error(), "interactive protected principal approval") {
					t.Fatalf("did not reach interactive admission: %v", e)
				}
			case "missing-auth":
				if !errors.Is(e, os.ErrNotExist) {
					t.Fatalf("did not reach verifier load: %v", e)
				}
			case "wrong-password":
				if !errors.Is(e, principalauth.ErrAuthentication) {
					t.Fatalf("did not reach password verification: %v", e)
				}
			}
			if scenario == "confirmation" && !strings.Contains(e.Error(), "confirmation rejected") {
				t.Fatalf("did not reach confirmation: %v", e)
			}
			if runner.writes != 0 || string(mustCommandRead(t, plan.UnitPath)) != string(foreign) {
				t.Fatal("rejected CLI mutated unit")
			}
			entries, _ := os.ReadDir(filepath.Dir(plan.UnitPath))
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".aegis-recovery-") {
					t.Fatal("evidence written on denial")
				}
			}
		})
	}
}
