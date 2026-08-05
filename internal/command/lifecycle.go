package command

import (
	"fmt"

	"github.com/berryhill/aegis/internal/config"
	"github.com/spf13/cobra"
)

type lifecyclePath string

const (
	lifecycleInitialize lifecyclePath = "initialize"
	lifecycleMigrate    lifecyclePath = "migrate-layout"
	lifecycleReset      lifecyclePath = "reset"
	lifecycleStart      lifecyclePath = "start"
	lifecycleRepair     lifecyclePath = "repair"
	lifecycleUtility    lifecyclePath = "utility"
)

// classifyLifecycle is the pre-configuration dispatch boundary. It performs
// artifact discovery only; ordinary application stores and runtime services are
// constructed after this classification has selected a safe path.
func classifyLifecycle(cmd *cobra.Command, configPath string) (lifecyclePath, config.Inspection, error) {
	switch cmd.Name() {
	case "reset":
		return lifecycleReset, config.Inspect(configPath), nil
	case "migrate-layout":
		inspection := config.Inspect(configPath)
		if inspection.State != config.StateLegacy {
			return lifecycleMigrate, inspection, fmt.Errorf("layout migration requires legacy-only state; discovered %s", inspection.State)
		}
		return lifecycleMigrate, inspection, nil
	case "help", "version", "update", "credential-bridge":
		return lifecycleUtility, config.Inspection{}, nil
	}

	inspection := config.Inspect(configPath)
	entrypoint := cmd.Parent() == nil || cmd.Name() == "manager" || cmd.Name() == "init"
	if entrypoint {
		switch inspection.State {
		case config.StateAbsent, config.StatePartial:
			return lifecycleInitialize, inspection, nil
		case config.StateLegacy:
			return lifecycleMigrate, inspection, fmt.Errorf("%s: run 'aegis migrate-layout' or 'aegis reset' before initialization", inspection.ReasonCode)
		case config.StateValid:
			return lifecycleStart, inspection, nil
		default:
			return lifecycleRepair, inspection, nil
		}
	}
	if inspection.State != config.StateValid {
		if inspection.State == config.StateLegacy {
			return lifecycleMigrate, inspection, fmt.Errorf("%s: run 'aegis migrate-layout' or 'aegis reset' before ordinary commands", inspection.ReasonCode)
		}
		return lifecycleRepair, inspection, fmt.Errorf("ordinary command denied before configuration is valid: %w", inspection.Failure())
	}
	return lifecycleStart, inspection, nil
}
