package command

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/onboarding"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

func userServiceCmd(runner userservice.Runner, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "gateway", Aliases: []string{"service"}, Short: "Manage the authenticated Aegis gateway", Args: cobra.NoArgs}
	preview := func() (userservice.Plan, error) {
		executable, err := os.Executable()
		if err != nil {
			return userservice.Plan{}, err
		}
		return userservice.Preview(executable, options.configFile)
	}
	command.AddCommand(&cobra.Command{Use: "preview", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		plan, err := preview()
		if err != nil {
			return err
		}
		return output(cmd, plan)
	}})
	command.AddCommand(&cobra.Command{Use: "install", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New("gateway installation requires an interactive principal approval; no mutations were performed"))
		}
		plan, err := preview()
		if err != nil {
			return err
		}
		approved, err := approveServicePlan(cmd, plan, newTerminalInput(cmd.InOrStdin()))
		if err != nil || !approved {
			return err
		}
		if err = userservice.Apply(cmd.Context(), plan, runner, 20*time.Second); err != nil {
			return err
		}
		return output(cmd, userservice.LifecycleResult{Action: "install", Unit: userservice.UnitName, UnitPath: plan.UnitPath, UnitDigest: plan.UnitDigest, Active: true, Ready: true, AuditCurrent: true})
	}})
	command.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		plan, err := preview()
		if err != nil {
			return err
		}
		status, err := userservice.Status(cmd.Context(), runner, plan)
		if err != nil {
			return err
		}
		return output(cmd, status)
	}})
	for _, action := range []string{"start", "stop", "restart"} {
		action := action
		command.AddCommand(&cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
				return usage(errors.New("gateway state changes require an interactive terminal"))
			}
			plan, err := preview()
			if err != nil {
				return err
			}
			result, err := userservice.Action(cmd.Context(), runner, plan, action, 20*time.Second)
			if err != nil {
				return err
			}
			return output(cmd, result)
		}})
	}
	command.AddCommand(&cobra.Command{Use: "uninstall", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New("gateway uninstall requires an interactive principal approval; no mutations were performed"))
		}
		plan, err := preview()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Disable and remove exact Aegis-owned unit %s while preserving configuration, state, and external credentials? [y/N]: ", plan.UnitPath)
		answer, _, err := newTerminalInput(cmd.InOrStdin()).ReadLine(cmd.Context(), 16)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "yes" && strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Fprintln(cmd.OutOrStdout(), "Gateway uninstall declined; no mutations were performed.")
			return nil
		}
		if err = userservice.Uninstall(cmd.Context(), plan, runner); err != nil {
			return err
		}
		return output(cmd, userservice.LifecycleResult{Action: "uninstall", Unit: userservice.UnitName, UnitPath: plan.UnitPath, UnitDigest: plan.UnitDigest})
	}})
	return command
}

func reconcileServeTransport(cmd *cobra.Command, configPath string, input *terminalInput) (bool, error) {
	inspection := config.Inspect(configPath)
	if inspection.State != config.StateValid || inspection.Config.API.Token != "" && inspection.Config.API.UnixSocket != "" {
		return true, nil
	}
	plan, err := onboarding.PreviewTransport(configPath)
	if err != nil {
		return false, usage(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nProtected control-plane transport reconciliation\nConfiguration: %s\nToken custody: %s (new owner-only file; value will not be displayed)\nUnix socket: %s\nOriginal digest: %s\nResult digest: %s\nApply this transport reconciliation? [Y/n]: ", plan.ConfigPath, plan.TokenPath, plan.UnixSocket, plan.OriginalDigest, plan.ResultDigest)
	approved, err := readDefaultYes(cmd, input)
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "Serve transport reconciliation declined; no mutations were performed.")
		return false, err
	}
	if err = onboarding.ApplyTransport(cmd.Context(), plan); err != nil {
		return false, err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Protected serve transport reconciled without displaying reusable bearer material.")
	return true, nil
}

func approveServicePlan(cmd *cobra.Command, plan userservice.Plan, input *terminalInput) (bool, error) {
	fmt.Fprintf(cmd.OutOrStdout(), "Aegis gateway installation preview\nPrincipal: %s\nUnit: %s\nDigest: %s\nExecutable: %s\nConfiguration: %s\nConsole: %s\nScope: systemctl --user; lingering will not be enabled.\nInstall and activate this gateway? [Y/n]: ", plan.Principal, plan.UnitPath, plan.UnitDigest, plan.Executable, plan.ConfigPath, plan.Origin)
	approved, err := readDefaultYes(cmd, input)
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "Gateway installation declined; no gateway state was changed.")
		return false, err
	}
	return true, nil
}

func consoleCmd(options *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "console", Short: "Locate the password-gated browser console", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		result, err := consoleLoginTarget(options.configFile)
		if err != nil {
			return err
		}
		return output(cmd, result)
	}}
}

func consoleLoginTarget(configPath string) (map[string]any, error) {
	cfg, err := config.Load(configPath, nil)
	if err != nil {
		return nil, err
	}
	origin := strings.TrimRight(cfg.API.Console.Origin, "/")
	if origin == "" {
		return nil, errors.New("control_plane_unavailable: console origin is not configured")
	}
	return map[string]any{
		"console_origin":          cfg.API.Console.Origin,
		"login_url":               origin + "/console",
		"authentication_required": "principal_password",
	}, nil
}
