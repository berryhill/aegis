package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/config"
	resetdomain "github.com/berryhill/aegis/internal/reset"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

func resetCmd(service *resetdomain.Service, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions, profile ExecutionProfile) *cobra.Command {
	return resetCmdWithRunner(service, userservice.Systemctl{}, isTerminal, options, profile)
}

type resetAuthenticator func(*cobra.Command, resetdomain.Plan) error
type resetGatewayPurger func(context.Context, string) (bool, error)
type resetGatewayPreparer func(*cobra.Command, string) error

var errResetPreparationDeclined = errors.New("reset preparation declined")

func resetCmdWithRunner(service *resetdomain.Service, runner userservice.Runner, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions, profile ExecutionProfile) *cobra.Command {
	purge := func(ctx context.Context, configPath string) (bool, error) {
		executable, err := os.Executable()
		if err != nil {
			return false, err
		}
		return userservice.PurgeForReset(ctx, executable, configPath, runner)
	}
	prepare := func(cmd *cobra.Command, configPath string) error {
		inspection := config.Inspect(configPath)
		if inspection.State != config.StateValid {
			return nil
		}
		present, err := userservice.UnitPresent()
		if err != nil {
			return err
		}
		if !present {
			return nil
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		plan, err := userservice.Preview(executable, configPath)
		if err != nil {
			return err
		}
		installed, err := userservice.Installed(plan)
		if err != nil {
			return err
		}
		if !installed {
			return nil
		}
		if profile != DevelopmentProfile {
			return usage(errors.New("gateway_must_be_stopped_for_reset_preview: run 'aegis gateway stop', then rerun 'aegis reset'; no writes were performed"))
		}
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New(resetdomain.ReasonRequiresTTY + ": reset preparation requires real terminal input and output; no writes were performed"))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Reset preparation must stop the exact Aegis gateway before credential-authority ownership can be verified.\nUnit: %s\nDigest: %s\nStop this exact gateway and continue to the reset preview? [y/N]: ", plan.UnitPath, plan.UnitDigest)
		answer, eof, readErr := newTerminalInput(cmd.InOrStdin()).ReadLine(cmd.Context(), 64)
		if readErr != nil {
			return fmt.Errorf("%s: reset-preparation confirmation failed; no writes were performed: %w", resetdomain.ReasonDeclined, readErr)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if eof || answer != "y" && answer != resetdomain.Confirmation {
			if err := output(cmd, map[string]any{"state": "unchanged", "reason": resetdomain.ReasonDeclined, "gateway_stopped": false, "written": false}); err != nil {
				return err
			}
			return errResetPreparationDeclined
		}
		result, err := userservice.Action(cmd.Context(), runner, plan, "stop", 20*time.Second)
		if err != nil {
			return fmt.Errorf("gateway_stop_for_reset_preview_failed: reset state was preserved: %w", err)
		}
		return output(cmd, result)
	}
	return resetCmdWithPreparation(service, isTerminal, options, profile, authenticateResetAuthority, prepare, purge)
}

func resetCmdWithAuthenticator(service *resetdomain.Service, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions, profile ExecutionProfile, authenticate resetAuthenticator) *cobra.Command {
	purge := func(ctx context.Context, configPath string) (bool, error) {
		executable, err := os.Executable()
		if err != nil {
			return false, err
		}
		return userservice.PurgeForReset(ctx, executable, configPath, userservice.Systemctl{})
	}
	return resetCmdWithHooks(service, isTerminal, options, profile, authenticate, purge)
}

func resetCmdWithHooks(service *resetdomain.Service, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions, profile ExecutionProfile, authenticate resetAuthenticator, purgeGateway resetGatewayPurger) *cobra.Command {
	return resetCmdWithPreparation(service, isTerminal, options, profile, authenticate, nil, purgeGateway)
}

func resetCmdWithPreparation(service *resetdomain.Service, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions, profile ExecutionProfile, authenticate resetAuthenticator, prepareGateway resetGatewayPreparer, purgeGateway resetGatewayPurger) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Return Aegis-owned local onboarding state to uninitialized",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			configuredPath := options.configFile
			configFlag := cmd.Flags().Lookup("config")
			if configFlag == nil {
				configFlag = cmd.InheritedFlags().Lookup("config")
			}
			if profile == ProductionProfile && configFlag != nil && !configFlag.Changed {
				configuredPath = ""
			}
			if prepareGateway != nil {
				if err := prepareGateway(cmd, configuredPath); err != nil {
					if errors.Is(err, errResetPreparationDeclined) {
						return nil
					}
					return err
				}
			}
			plan, err := service.Plan(cmd.Context(), configuredPath)
			if err != nil {
				return usage(err)
			}
			requiresAuthority := profile != DevelopmentProfile
			confirmation := "y/yes"
			authorityAuthentications := 0
			if requiresAuthority {
				confirmation = "authority passphrase, then y/yes, then authority passphrase again"
				authorityAuthentications = 2
			}
			preview := struct {
				Operation            string                 `json:"operation"`
				PlanDigest           string                 `json:"plan_digest"`
				Authenticated        resetdomain.Principal  `json:"authenticated_principal"`
				ConfigPath           string                 `json:"resolved_config_path"`
				ConfigState          string                 `json:"configuration_state"`
				Delete               []resetdomain.Artifact `json:"delete"`
				Preserve             []string               `json:"preserve"`
				CredentialRecords    bool                   `json:"credential_records_destroyed"`
				LocalKEK             bool                   `json:"local_kek_destroyed"`
				Postcondition        string                 `json:"postcondition"`
				Warning              string                 `json:"warning"`
				RetainedLegacy       []string               `json:"retained_empty_legacy_directories,omitempty"`
				ExecutionProfile     ExecutionProfile       `json:"execution_profile,omitempty"`
				AuthorityPassphrase  bool                   `json:"authority_passphrase_required"`
				AuthorityPrompts     int                    `json:"authority_passphrase_authentications"`
				ConfirmationRequired string                 `json:"confirmation_required"`
				GatewayAction        string                 `json:"gateway_action"`
			}{"reset", resetdomain.PlanDigest(plan), plan.Principal, plan.ConfigPath, string(plan.ConfigState), plan.Artifacts, plan.Preserved, plan.CredentialRecords, plan.LocalKEK, plan.Postcondition, plan.Warning, plan.LegacyRetained, profile, requiresAuthority, authorityAuthentications, confirmation, "stop and purge exact Aegis-owned user gateway if installed"}
			if err = output(cmd, preview); err != nil {
				return err
			}
			if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
				return usage(errors.New(resetdomain.ReasonRequiresTTY + ": reset requires real terminal input and output; no writes were performed"))
			}
			if requiresAuthority {
				if err = authenticate(cmd, plan); err != nil {
					return err
				}
			}
			fmt.Fprint(cmd.OutOrStdout(), "Apply this exact reset plan? [y/N]: ")
			answer, eof, readErr := newTerminalInput(cmd.InOrStdin()).ReadLine(cmd.Context(), 64)
			if readErr != nil {
				return fmt.Errorf("%s: confirmation input failed; no writes were performed: %w", resetdomain.ReasonDeclined, readErr)
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if eof || answer != "y" && answer != resetdomain.Confirmation {
				return output(cmd, map[string]any{"state": "unchanged", "reason": resetdomain.ReasonDeclined, "written": false})
			}
			if requiresAuthority {
				if err = authenticate(cmd, plan); err != nil {
					return err
				}
			}
			gatewayPurged, err := purgeGateway(cmd.Context(), plan.ConfigPath)
			if err != nil {
				return fmt.Errorf("gateway_stop_and_purge_failed: reset state was preserved: %w", err)
			}
			if err = service.Apply(cmd.Context(), plan); err != nil {
				return err
			}
			return output(cmd, map[string]any{"state": "uninitialized", "reason": "reset_complete", "gateway_purged": gatewayPurged, "next_command": "aegis", "retained_empty_legacy_directories": plan.LegacyRetained})
		},
	}
}

func authenticateResetAuthority(cmd *cobra.Command, plan resetdomain.Plan) error {
	inspection := config.Inspect(plan.ConfigPath)
	if inspection.State != config.StateValid || inspection.Config.Credentials.Authority.Custody != "passphrase-file" {
		return usage(fmt.Errorf("%s: production reset requires a verifiable passphrase-file authority; no writes were performed", resetdomain.ReasonRequiresAuthority))
	}
	custodian, err := loadConfiguredCustodian(cmd, inspection.Config.Credentials.Authority)
	if err != nil {
		return fmt.Errorf("%s: authority passphrase authentication failed; no writes were performed: %w", resetdomain.ReasonRequiresAuthority, err)
	}
	custodian.Close()
	return nil
}
