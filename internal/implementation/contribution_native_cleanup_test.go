package implementation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/execution"
)

// Exercise the actual native check caller, not a replacement cleanup helper.
func TestContributionNativeUnknownRetainsPrivateWorkspace(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "joined", true: "unknown"}[fail], func(t *testing.T) {
			ctx := context.Background()
			kernel, c := fixture(t)
			source, err := Inventory(ctx, c.Workspace)
			if err != nil {
				t.Fatal(err)
			}
			home := ""
			e := &ManagedContribution{DB: kernel.DB, GoBinary: kernel.GoBinary, StateRoot: t.TempDir(), Admit: kernel.Admit}
			e.Native = func(_ context.Context, cmd *exec.Cmd) error {
				home = filepath.Dir(cmd.Dir)
				if _, err := os.Stat(cmd.Dir); err != nil {
					t.Fatal(err)
				}
				if fail {
					return execution.ErrNativeQuiescenceUnavailable
				}
				return nil
			}
			_, err = e.native(ctx, c, source, nil)
			if fail && !errors.Is(err, execution.ErrNativeQuiescenceUnavailable) {
				t.Fatal(err)
			}
			if !fail && err != nil {
				t.Fatal(err)
			}
			_, statErr := os.Stat(home)
			if fail && statErr != nil {
				t.Fatalf("unknown writer workspace removed: %v", statErr)
			}
			if !fail && !os.IsNotExist(statErr) {
				t.Fatalf("joined workspace retained: %v", statErr)
			}
		})
	}
}
