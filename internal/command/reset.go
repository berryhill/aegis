package command

import (
	"errors"
	"fmt"
	"io"

	resetdomain "github.com/berryhill/aegis/internal/reset"
	"github.com/spf13/cobra"
)

func resetCmd(service *resetdomain.Service, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Return Aegis-owned local onboarding state to uninitialized",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := service.Plan(cmd.Context(), options.configFile)
			if err != nil {
				return usage(err)
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
				ConfirmationRequired string                 `json:"confirmation_required"`
			}{"reset", resetdomain.PlanDigest(plan), plan.Principal, plan.ConfigPath, string(plan.ConfigState), plan.Artifacts, plan.Preserved, plan.CredentialRecords, plan.LocalKEK, plan.Postcondition, plan.Warning, resetdomain.Confirmation}
			if err = output(cmd, preview); err != nil {
				return err
			}
			if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
				return usage(errors.New(resetdomain.ReasonRequiresTTY + ": reset requires real terminal input and output; no writes were performed"))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Type %s to apply this exact reset plan: ", resetdomain.Confirmation)
			answer, eof, readErr := newTerminalInput(cmd.InOrStdin()).ReadLine(cmd.Context(), 64)
			if readErr != nil {
				return fmt.Errorf("%s: confirmation input failed; no writes were performed: %w", resetdomain.ReasonDeclined, readErr)
			}
			if eof || answer != resetdomain.Confirmation {
				return output(cmd, map[string]any{"state": "unchanged", "reason": resetdomain.ReasonDeclined, "written": false})
			}
			if err = service.Apply(cmd.Context(), plan); err != nil {
				return err
			}
			return output(cmd, map[string]any{"state": "uninitialized", "reason": "reset_complete", "next_command": "aegis"})
		},
	}
}
