package command

import (
	"bytes"
	"context"
	"io"
	"testing"

	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

type sliceUpdater struct{ applied bool }

func (u *sliceUpdater) Run(_ context.Context, check bool) (selfupdate.Result, error) {
	if !check {
		u.applied = true
	}
	return selfupdate.Result{}, nil
}
func TestUpgradeUnavailableBoundaryDoesNotApply(t *testing.T) {
	u := &sliceUpdater{}
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(bytes.NewBuffer(nil))
	cmd.SetOut(io.Discard)
	err := runUpdate(cmd, u, nil, func(context.Context, userservice.Runner, string, string) string { return "unknown" }, "missing-synthetic-config", false)
	if err == nil || u.applied {
		t.Fatalf("unavailable upgrade boundary: err=%v applied=%v", err, u.applied)
	}
}
