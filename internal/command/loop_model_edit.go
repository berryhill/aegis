package command

import (
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/spf13/cobra"
)

func loopModelEditCommands() []*cobra.Command {
	online := func(*cobra.Command, []string) error {
		return errors.New("control_plane_online_required: select the owning service with --target")
	}
	update := &cobra.Command{Use: "model-update LOOP REVISION", Short: "Edit an owned Loop's Hermes model; retain an exact independent review if needed", Args: cobra.ExactArgs(2), RunE: online}
	update.Flags().String("agent", "", "Exact registered owner Agent ID")
	update.Flags().String("provider", "", "Selected provider (no fallback)")
	update.Flags().String("model", "", "Selected exact model")
	update.Flags().StringSlice("step", nil, "Hermes step IDs; omit to update all declared Hermes turns")
	return []*cobra.Command{update,
		{Use: "edit FILE", Short: "Submit an exact owned Loop edit without bypassing model approval", Args: cobra.ExactArgs(1), RunE: online},
		{Use: "edit-show ID", Short: "Read the exact retained model edit", Args: cobra.ExactArgs(1), RunE: online},
		{Use: "edit-publish ID", Short: "Publish the retained revision after independent model approval", Args: cobra.ExactArgs(1), RunE: online},
	}
}

func loopModelUpdateInput(cmd *cobra.Command) (app.OwnedLoopModelUpdateInput, error) {
	in := app.OwnedLoopModelUpdateInput{}
	in.AgentID, _ = cmd.Flags().GetString("agent")
	in.Provider, _ = cmd.Flags().GetString("provider")
	in.Model, _ = cmd.Flags().GetString("model")
	in.Steps, _ = cmd.Flags().GetStringSlice("step")
	if in.AgentID == "" || in.Provider == "" || in.Model == "" {
		return in, errors.New("--agent, --provider and --model are required")
	}
	return in, nil
}
