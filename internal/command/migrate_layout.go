package command

import (
	"errors"
	"fmt"
	"io"

	"github.com/berryhill/aegis/internal/migration"
	"github.com/spf13/cobra"
)

func migrateLayoutCmd(service *migration.Service, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "migrate-layout",
		Short: "Migrate an exact legacy local installation to ~/.argis",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if options.configFile != "" {
				return usage(errors.New("migrate-layout does not infer migration authority from --config; inspect that explicit deployment manually"))
			}
			plan, err := service.Plan(cmd.Context())
			if err != nil {
				return usage(err)
			}
			if err = output(cmd, map[string]any{"operation": "migrate-layout", "plan_digest": migration.Digest(plan), "source_config": plan.SourceConfig, "source_state": plan.SourceState, "source_checkpoints": plan.SourceCheckpoints, "destination_root": plan.DestinationRoot, "destination_config": plan.DestinationConfig, "destination_state": plan.DestinationState, "artifacts": plan.Artifacts, "preserve": plan.Preserved, "confirmation_required": migration.Confirmation}); err != nil {
				return err
			}
			if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
				return usage(errors.New("migration_requires_tty: migration requires real terminal input and output; no writes were performed"))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Type exactly %q to apply this digest-bound migration plan: ", migration.Confirmation)
			answer, eof, err := newTerminalInput(cmd.InOrStdin()).ReadLine(cmd.Context(), 128)
			if err != nil {
				return err
			}
			if eof || answer != migration.Confirmation {
				return output(cmd, map[string]any{"state": "legacy-layout-detected", "reason": "migration_declined", "written": false})
			}
			if err = service.Apply(cmd.Context(), plan); err != nil {
				return err
			}
			return output(cmd, map[string]any{"state": "canonical", "reason": "migration_complete", "config": plan.DestinationConfig, "state_dir": plan.DestinationState})
		},
	}
}
