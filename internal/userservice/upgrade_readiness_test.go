package userservice

import (
	"context"
	"github.com/berryhill/aegis/internal/config"
	"strings"
	"testing"
	"time"
)

type crashedUpgradeRunner struct{}

func (crashedUpgradeRunner) Run(context.Context, ...string) error { return nil }
func (crashedUpgradeRunner) Output(_ context.Context, args ...string) ([]byte, error) {
	switch args[3] {
	case "ActiveState":
		return []byte("failed"), nil
	case "ExecMainStatus":
		return []byte("23"), nil
	case "Result":
		return []byte("exit-code"), nil
	}
	return nil, nil
}
func TestUpgradeReadinessReportsExitedServiceWithoutAuditTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cfg := config.Defaults()
	cfg.API.UnixSocket = "/nonexistent-upgrade-fixture.sock"
	started := time.Now()
	err := waitServiceReady(ctx, crashedUpgradeRunner{}, cfg)
	if err == nil || !strings.Contains(err.Error(), "exit_status=\"23\"") || time.Since(started) > time.Second {
		t.Fatalf("err=%v elapsed=%s", err, time.Since(started))
	}
}
