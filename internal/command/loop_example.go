package command

import (
	_ "embed"
	"fmt"
	"github.com/spf13/cobra"
)

//go:embed loop_example.json
var loopExample []byte

func loopExampleCmd() *cobra.Command {
	return &cobra.Command{Use: "example", Short: "Print the installed typed basic-implementation publication template (not executable code-task semantics)", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		fmt.Fprintln(cmd.ErrOrStderr(), "Definition template only: empty evidence requirements do not prove verification. task/acceptance_criteria are typed ports, not executable instructions or bounded values; max_attempts=2 is a declaration, not an enforced corrective attempt. No code-task instruction binding or verification-success semantics are provided. Select an existing enabled Agent with the owner. Publication is not activation or execution.")
		_, err := cmd.OutOrStdout().Write(loopExample)
		return err
	}}
}
