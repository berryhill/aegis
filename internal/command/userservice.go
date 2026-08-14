package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/onboarding"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

func userServiceCmd(runner userservice.Runner, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "service", Short: "Manage the authenticated Aegis user service", Args: cobra.NoArgs}
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
			return usage(errors.New("user service installation requires an interactive principal approval; no mutations were performed"))
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
		return output(cmd, map[string]any{"installed": true, "unit": userservice.UnitName, "unit_digest": plan.UnitDigest, "console_origin": plan.Origin, "reusable_secret_exposed": false})
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
				return usage(errors.New("user service state changes require an interactive terminal"))
			}
			plan, err := preview()
			if err != nil {
				return err
			}
			return userservice.Action(cmd.Context(), runner, plan, action)
		}})
	}
	command.AddCommand(&cobra.Command{Use: "uninstall", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New("user service uninstall requires an interactive principal approval; no mutations were performed"))
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
			fmt.Fprintln(cmd.OutOrStdout(), "User service uninstall declined; no mutations were performed.")
			return nil
		}
		return userservice.Uninstall(cmd.Context(), plan, runner)
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
	fmt.Fprintf(cmd.OutOrStdout(), "Aegis user-service installation preview\nPrincipal: %s\nUnit: %s\nDigest: %s\nExecutable: %s\nConfiguration: %s\nConsole: %s\nScope: systemctl --user; lingering will not be enabled.\nInstall and activate this user service? [Y/n]: ", plan.Principal, plan.UnitPath, plan.UnitDigest, plan.Executable, plan.ConfigPath, plan.Origin)
	approved, err := readDefaultYes(cmd, input)
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "User service installation declined; no service state was changed.")
		return false, err
	}
	return true, nil
}

func consoleCmd(options *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "console", Short: "Obtain a single-use authenticated console bootstrap", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		result, err := obtainConsoleBootstrap(cmd.Context(), options.configFile)
		if err != nil {
			return err
		}
		return output(cmd, result)
	}}
}

func obtainConsoleBootstrap(ctx context.Context, configPath string) (map[string]any, error) {
	cfg, err := config.Load(configPath, nil)
	if err != nil {
		return nil, err
	}
	if cfg.API.UnixSocket == "" || cfg.API.Token == "" {
		return nil, errors.New("control_plane_unavailable: protected Unix transport is not configured")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
	}}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/console/bootstrap", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.API.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("control_plane_unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("console bootstrap denied by control plane: HTTP %d", response.StatusCode)
	}
	var issued struct {
		Bootstrap string `json:"bootstrap"`
		ExpiresAt string `json:"expires_at"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&issued); err != nil || issued.Bootstrap == "" {
		return nil, errors.New("control plane returned an invalid console bootstrap")
	}
	return map[string]any{"console_origin": cfg.API.Console.Origin, "bootstrap": issued.Bootstrap, "expires_at": issued.ExpiresAt, "single_use": true, "reusable_bearer_exposed": false}, nil
}

func launchConsole(cmd *cobra.Command, options *rootOptions, opener BrowserOpener) error {
	result, err := obtainConsoleBootstrap(cmd.Context(), options.configFile)
	if err != nil {
		return err
	}
	origin, _ := result["console_origin"].(string)
	target := strings.TrimRight(origin, "/") + "/console"
	if err = opener(cmd.Context(), target); err != nil {
		result["browser_opened"] = false
		result["manual_url"] = target
		if outputErr := output(cmd, result); outputErr != nil {
			return errors.Join(err, outputErr)
		}
		return fmt.Errorf("browser launch failed; open %s and enter the emitted single-use bootstrap manually: %w", target, err)
	}
	result["browser_opened"] = true
	return output(cmd, result)
}
