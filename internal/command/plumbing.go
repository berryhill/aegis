package command

import (
	"errors"
	"fmt"
	"os"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/spf13/cobra"
)

func plumbingCmd(build builder) *cobra.Command {
	command := &cobra.Command{Use: "plumbing", Short: "Run and inspect explicit non-production plumbing proofs"}
	var promptFile, expectFile, provider, model string
	var acknowledge bool
	pocCommand := &cobra.Command{
		Use:   "poc",
		Short: "Run one authenticated, non-restrictive, hermetic plumbing proof",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !acknowledge {
				return usage(errors.New("--acknowledge-plumbing-unrestricted is required"))
			}
			prompt, err := readBoundedProofFile(promptFile, hermes.MaxAttemptInputBytes)
			if err != nil {
				return err
			}
			expected, err := readBoundedProofFile(expectFile, hermes.MaxAttemptOutputBytes)
			if err != nil {
				return err
			}
			service, err := build(cmd)
			if err != nil {
				return err
			}
			subject, err := service.Authenticate(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.RunPlumbingPOC(cmd.Context(), subject, app.PlumbingPOCInput{Prompt: string(prompt), Expected: string(expected), Provider: provider, Model: model, Acknowledge: true})
			if err != nil {
				return err
			}
			return output(cmd, result)
		},
	}
	pocCommand.Flags().StringVar(&promptFile, "prompt-file", "", "file containing the prompt (never exposed in argv)")
	pocCommand.Flags().StringVar(&expectFile, "expect-file", "", "file containing the exact expected output")
	pocCommand.Flags().StringVar(&provider, "provider", "", "configured Hermes provider")
	pocCommand.Flags().StringVar(&model, "model", "", "Hermes model")
	pocCommand.Flags().BoolVar(&acknowledge, "acknowledge-plumbing-unrestricted", false, "acknowledge this non-production proof uses explicit non-restrictive authority")
	for _, name := range []string{"prompt-file", "expect-file", "provider", "model"} {
		_ = pocCommand.MarkFlagRequired(name)
	}
	show := &cobra.Command{
		Use:   "show GRAPH_RUN_ID",
		Short: "Read back one validated terminal plumbing graph run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service, err := build(cmd)
			if err != nil {
				return err
			}
			subject, err := service.Authenticate(cmd.Context())
			if err != nil {
				return err
			}
			result, err := service.ReadGraphRun(cmd.Context(), subject, args[0])
			if err != nil {
				return err
			}
			return output(cmd, result)
		},
	}
	command.AddCommand(pocCommand, show)
	return command
}

func readBoundedProofFile(path string, limit int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > int64(limit) {
		return nil, fmt.Errorf("proof input %q must be a non-empty regular file no larger than %d bytes", path, limit)
	}
	return os.ReadFile(path)
}
