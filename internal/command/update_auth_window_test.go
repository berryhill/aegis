//go:build linux

package command

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type driftUpgradeInput struct{ change func() }

func (p *driftUpgradeInput) Acquire(context.Context, AuthorityPassphraseRequest) ([]byte, error) {
	p.change()
	return []byte("synthetic-recovery-password"), nil
}

func TestInstalledUpgradeAuthenticationWindowRejectsDrift(t *testing.T) {
	builds := t.TempDir()
	old := upgradeReleaseBuild(t, builds, "0.2.28")
	candidate := upgradeReleaseBuild(t, builds, "0.2.29")
	f := newUpgradeCommandFixture(t, old, candidate)
	installLegacyUpgradeFixture(t, f)
	unitBefore := sha256.Sum256(mustCommandRead(t, f.runner.plan.UnitPath))
	imageBefore := sha256.Sum256(mustCommandRead(t, f.target))
	provider := &driftUpgradeInput{change: func() {
		paths, err := filepath.Glob(filepath.Join(filepath.Dir(f.target), ".aegis-update-*"))
		if err != nil || len(paths) != 1 {
			t.Fatalf("prepared lookup=%v %v", paths, err)
		}
		file, err := os.OpenFile(paths[0], os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = file.Write([]byte("changed during protected authentication"))
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("write=%v close=%v", err, closeErr)
		}
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := f.run(t, ctx, provider)
	if err == nil || result.UpgradeComplete {
		t.Fatalf("drift admitted: %+v %v", result, err)
	}
	for _, call := range f.runner.calls {
		if call[0] == "stop" || call[0] == "start" || call[0] == "daemon-reload" {
			t.Fatalf("authentication-window drift mutated service: %q", call)
		}
	}
	if sha256.Sum256(mustCommandRead(t, f.runner.plan.UnitPath)) != unitBefore || sha256.Sum256(mustCommandRead(t, f.target)) != imageBefore {
		t.Fatal("authentication-window drift changed installation")
	}
}
