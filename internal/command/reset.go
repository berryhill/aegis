package command

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/berryhill/aegis/internal/config"
	resetdomain "github.com/berryhill/aegis/internal/reset"
	"github.com/spf13/cobra"
)

func resetCmd(service *resetdomain.Service, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions, profile ExecutionProfile) *cobra.Command {
	return resetCmdWithAuthenticator(service, isTerminal, options, profile, authenticateResetAuthority)
}

type resetAuthenticator func(*cobra.Command, resetdomain.Plan) error

func resetCmdWithAuthenticator(service *resetdomain.Service, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions, profile ExecutionProfile, authenticate resetAuthenticator) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Return Aegis-owned local onboarding state to uninitialized",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := service.Plan(cmd.Context(), options.configFile)
			if err != nil {
				return usage(err)
			}
			requiresAuthority := profile != DevelopmentProfile && (plan.CredentialRecords || plan.LocalKEK)
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
			}{"reset", resetdomain.PlanDigest(plan), plan.Principal, plan.ConfigPath, string(plan.ConfigState), plan.Artifacts, plan.Preserved, plan.CredentialRecords, plan.LocalKEK, plan.Postcondition, plan.Warning, plan.LegacyRetained, profile, requiresAuthority, authorityAuthentications, confirmation}
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
			if err = service.Apply(cmd.Context(), plan); err != nil {
				return err
			}
			return output(cmd, map[string]any{"state": "uninitialized", "reason": "reset_complete", "next_command": "aegis", "retained_empty_legacy_directories": plan.LegacyRetained})
		},
	}
}

func authenticateResetAuthority(cmd *cobra.Command, plan resetdomain.Plan) error {
	if !plan.CredentialRecords && !plan.LocalKEK {
		return nil
	}
	inspection := config.Inspect(plan.ConfigPath)
	if inspection.State != config.StateValid || inspection.Config.Credentials.Authority.Custody != "passphrase-file" {
		return usage(fmt.Errorf("%s: reset would destroy credential authority material but no verifiable passphrase-file authority is configured; no writes were performed", resetdomain.ReasonRequiresAuthority))
	}
	custodian, err := loadConfiguredCustodian(cmd, inspection.Config.Credentials.Authority)
	if err != nil {
		return fmt.Errorf("%s: authority passphrase authentication failed; no writes were performed: %w", resetdomain.ReasonRequiresAuthority, err)
	}
	custodian.Close()
	return nil
}
