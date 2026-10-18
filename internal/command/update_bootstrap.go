package command

import (
	"errors"
	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
	"runtime"
)

func runBootstrapUpgrade(cmd *cobra.Command, updater UpdateService, runner userservice.Runner, configPath string) error {
	if runtime.GOOS != "linux" {
		return errors.New("installed gateway bootstrap requires Linux")
	}
	concrete, ok := updater.(*selfupdate.Updater)
	if !ok {
		return errors.New("verified release coordinator required")
	}
	executable, err := userservice.DiscoverInstalledExecutable(cmd.Context(), runner, configPath)
	if err != nil {
		return err
	}
	bound, err := concrete.ForInstalled(executable)
	if err != nil {
		return err
	}
	return runInstalledUpgrade(cmd, bound, runner, configPath)
}
