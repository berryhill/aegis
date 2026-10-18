//go:build linux

package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/spf13/cobra"
)

func TestInstalledBootstrapOfficialOld(t *testing.T) {
	old := os.Getenv("AEGIS_TEST_OLD_RELEASE")
	if old == "" {
		t.Skip("set AEGIS_TEST_OLD_RELEASE to checksum-verified official v0.2.29")
	}
	image := mustCommandRead(t, old)
	if got := sha256.Sum256(image); strings.ToLower(fmt.Sprintf("%x", got)) != "377cda58584dffb5ff5ab739d7758ad8bc87ee427e1aa34b55b5b311f38033eb" {
		t.Fatal("wrong official fixture")
	}
	candidate := upgradeReleaseBuild(t, t.TempDir(), "0.2.31")
	f := newVersionedUpgradeCommandFixture(t, old, candidate, "0.2.31")
	id := selfupdate.CandidateIdentity{Executable: f.target, Version: "0.2.30", Source: strings.Repeat("b", 40), Digest: strings.Repeat("c", 64)}
	j, err := openUpgradeJournal(f.runner.plan, f.state, sha256.Sum256(mustCommandRead(t, f.path)), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.save(); err != nil {
		t.Fatal(err)
	}
	j.close()
	// Execute the checksum-pinned OLD official coordinator, not current command code.
	// This opt-in subprocess uses public GitHub release downloads; all config/state,
	// executable and service-manager responses are private fixture paths.
	originalUnit := mustCommandRead(t, f.runner.plan.UnitPath)
	installLegacyUpgradeFixture(t, f)
	fakebin := filepath.Join(filepath.Dir(f.path), "fakebin")
	execStart := fmt.Sprintf("{ path=%s ; argv[]=%s serve --config %s ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }", f.target, f.target, f.path)
	fake := "#!/bin/sh\ncase \"$*\" in\n*FragmentPath*) printf '%s\\n' '" + f.runner.plan.UnitPath + "';;\n*ExecStart*) printf '%s\\n' '" + execStart + "';;\n*MainPID*) printf '0\\n';;\n*ActiveState*) printf 'inactive\\n';;\n*UnitFileState*) printf 'disabled\\n';;\n*) exit 91;;\nesac\n"
	upgradeWrite(t, filepath.Join(fakebin, "systemctl"), []byte(fake), 0700)
	oldcmd := exec.Command(f.target, "update", "--config", f.path)
	oldcmd.Env = append(os.Environ(), "PATH="+fakebin+":"+os.Getenv("PATH"))
	oldout, olderr := oldcmd.CombinedOutput()
	if olderr == nil || !strings.Contains(string(oldout), "conflict") {
		t.Fatalf("official old subprocess must deny conflicting retained intent: %v\n%s", olderr, oldout)
	}
	t.Logf("official old subprocess conflict: %s", oldout)
	// Restore the current recognized unit for the new coordinator acceptance path.
	upgradeWrite(t, f.runner.plan.UnitPath, originalUnit, 0600)
	// The old coordinator's exact non-reconciling journal admission remains denied.
	if _, err := openUpgradeJournal(f.runner.plan, f.state, sha256.Sum256(mustCommandRead(t, f.path)), selfupdate.CandidateIdentity{Executable: f.target, Version: "0.2.31", Source: strings.Repeat("a", 40), Digest: strings.Repeat("d", 64)}); err == nil {
		t.Fatal("old admission unexpectedly allowed conflicting intent")
	}
	f.updater.CurrentVersion = "0.2.31" // coordinator version must NOT decide target availability
	for attempt := 0; attempt < 2; attempt++ {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetContext(context.WithValue(context.Background(), authorityPassphraseContextKey{}, &recoveryPasswordFixture{}))
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		if err := runBootstrapUpgrade(cmd, f.updater, f.runner, f.path); err != nil {
			t.Fatal(err)
		}
		var result selfupdate.Result
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.UpgradeComplete || !result.ConsoleVerified || result.RunningVersion != "0.2.31" || result.Updated != (attempt == 0) {
			t.Fatalf("attempt %d: %+v", attempt, result)
		}
	}
	if _, err := os.Stat(filepath.Join(f.state, "upgrade", "journal.json")); err != nil {
		t.Fatal(err)
	}
}
