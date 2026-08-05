package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/spf13/cobra"
)

func lifecycleCommand(name string, entrypoint bool) *cobra.Command {
	cmd := &cobra.Command{Use: name}
	if !entrypoint {
		parent := &cobra.Command{Use: "aegis"}
		parent.AddCommand(cmd)
	}
	return cmd
}

func TestLifecycleClassificationSeparatesInitializationRepairAndOrdinaryStartup(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "aegis.yaml")

	path, inspection, err := classifyLifecycle(lifecycleCommand("aegis", true), configPath)
	if err != nil || path != lifecycleInitialize || inspection.State != config.StateAbsent {
		t.Fatalf("absent entrypoint path=%s inspection=%+v err=%v", path, inspection, err)
	}

	partial := filepath.Join(root, config.InitializationTemporaryPrefix+"interrupted")
	if err = os.WriteFile(partial, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	path, inspection, err = classifyLifecycle(lifecycleCommand("init", false), configPath)
	if err != nil || path != lifecycleInitialize || inspection.State != config.StatePartial {
		t.Fatalf("partial init path=%s inspection=%+v err=%v", path, inspection, err)
	}

	if err = os.Remove(partial); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, []byte("malformed: [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	path, inspection, err = classifyLifecycle(lifecycleCommand("aegis", true), configPath)
	if err != nil || path != lifecycleRepair || inspection.State != config.StateMalformed {
		t.Fatalf("malformed entrypoint path=%s inspection=%+v err=%v", path, inspection, err)
	}
	path, _, err = classifyLifecycle(lifecycleCommand("config", false), configPath)
	if err == nil || path != lifecycleRepair || !strings.Contains(err.Error(), "denied before configuration is valid") {
		t.Fatalf("malformed ordinary command path=%s err=%v", path, err)
	}
}

func TestLifecycleResetAndUtilityRemainAvailableWithoutConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "missing.yaml")
	for _, test := range []struct {
		name string
		want lifecyclePath
	}{{"reset", lifecycleReset}, {"help", lifecycleUtility}, {"version", lifecycleUtility}, {"update", lifecycleUtility}, {"credential-bridge", lifecycleUtility}} {
		path, _, err := classifyLifecycle(lifecycleCommand(test.name, false), configPath)
		if err != nil || path != test.want {
			t.Errorf("%s path=%s want=%s err=%v", test.name, path, test.want, err)
		}
	}
}
