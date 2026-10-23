package command

import (
	"errors"
	"github.com/spf13/cobra"
)

func controllerCodexCmd() *cobra.Command {
	c := &cobra.Command{Use: "controller-codex", Short: "Connect the controller to one exact existing native Hermes Codex login (online only)"}
	for _, name := range []string{"candidates", "preview", "connect", "show", "probe"} {
		use := name
		args := cobra.NoArgs
		if name == "preview" || name == "candidates" {
			use += " PROFILE_HOME"
			args = cobra.ExactArgs(1)
		}
		if name == "probe" {
			use += " FILE"
			args = cobra.ExactArgs(1)
		}
		if name == "connect" {
			use += " REVIEW_ID"
			args = cobra.ExactArgs(1)
		}
		child := &cobra.Command{Use: use, Args: args, RunE: func(*cobra.Command, []string) error {
			return errors.New("owning_service_required: use --config OWNER_CONFIG --target CONSOLE_URL; no local-store fallback")
		}}
		if name == "preview" {
			child.Flags().String("credential", "", "Exact opaque native credential ID returned by candidates (no default selection)")
			child.Flags().Bool("replace", false, "Review replacement of the exact current operational binding; preserve connection history")
		}
		c.AddCommand(child)
	}
	return c
}
