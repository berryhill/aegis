package command

import (
	"fmt"

	"github.com/berryhill/aegis/internal/skillbundle"
	"github.com/spf13/cobra"
)

// skillsCmd is an explicit filesystem utility, not a control-plane operation.
// The installer alone owns archive verification, destination safety and readback.
func skillsCmd() *cobra.Command {
	c := &cobra.Command{
		Use: "skills", Short: "Manage an explicit exact-artifact skills inventory", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	for _, verb := range []string{"install", "update", "rollback"} {
		c.AddCommand(&cobra.Command{
			Use:   verb + " ARCHIVE SHA256_DIGEST SOURCE_REVISION ABSOLUTE_HOME",
			Short: "Apply an exact verified archive to an explicit filesystem destination",
			Args:  cobra.ExactArgs(4),
			RunE: func(cmd *cobra.Command, args []string) error {
				result, err := skillbundle.InstallArchive(cmd.Context(), args[0], args[1], args[2], args[3])
				if err != nil {
					return err
				}
				return output(cmd, result)
			},
		})
	}
	c.AddCommand(&cobra.Command{
		Use: "inventory ABSOLUTE_HOME", Short: "Verify every byte of an explicit managed inventory",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := skillbundle.InspectInstalled(args[0])
			if err != nil {
				return err
			}
			return output(cmd, result)
		},
	})
	return c
}

func isSkillsCommand(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		if current.Name() == "skills" && current.Parent() != nil && current.Parent().Parent() == nil {
			return true
		}
	}
	return false
}

func skillsPreflight(cmd *cobra.Command) error {
	for _, name := range []string{"target", "config", "state-dir", "runtime", "hermes-executable", "pinentry-executable", "update", "version"} {
		if cmd.Flags().Changed(name) || cmd.InheritedFlags().Changed(name) {
			return usage(fmt.Errorf("skills filesystem utility cannot select --%s", name))
		}
	}
	return nil
}
