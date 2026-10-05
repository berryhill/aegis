package command

import (
	"github.com/berryhill/aegis/internal/app"
	"github.com/spf13/cobra"
)

func loopDoerServiceCommands(build builder) []*cobra.Command {
	readiness := &cobra.Command{Use: "readiness FILE", Short: "Inspect an exact Doer candidate without publication or approval", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input app.DoerReadinessInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		probe, _ := cmd.Flags().GetBool("probe")
		input.Probe = input.Probe || probe
		svc, err := build(cmd)
		if err != nil {
			return err
		}
		subject, err := svc.Authenticate(cmd.Context())
		if err != nil {
			return err
		}
		candidate := app.DoerCandidateReadinessInput{Agent: input.Agent, Candidate: input.Candidate}
		var value app.DoerCandidateReadiness
		if input.Probe {
			value, err = svc.ProbeDoerCandidateReadinessAs(cmd.Context(), subject, candidate)
		} else {
			value, err = svc.ReadDoerCandidateReadinessAs(cmd.Context(), subject, candidate)
		}
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	readiness.Flags().Bool("probe", false, "Explicitly probe the bounded local helper protocol")
	save := &cobra.Command{Use: "draft-save FILE", Short: "Retain a Doer proposal using its exact version guard", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input app.DoerDraftInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		svc, err := build(cmd)
		if err != nil {
			return err
		}
		subject, err := svc.Authenticate(cmd.Context())
		if err != nil {
			return err
		}
		value, err := svc.SaveDoerDraftAs(cmd.Context(), subject, input)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	show := &cobra.Command{Use: "draft-show ID", Short: "Read one retained Doer proposal", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := build(cmd)
		if err != nil {
			return err
		}
		subject, err := svc.Authenticate(cmd.Context())
		if err != nil {
			return err
		}
		value, err := svc.ReadDoerDraftAs(cmd.Context(), subject, args[0])
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	continuation := &cobra.Command{Use: "draft-continue FILE", Short: "Rebind a retained proposal to an already approved successor", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		var input app.ContinueDoerDraftInput
		if err := decodeJSONFile(args[0], &input); err != nil {
			return usage(err)
		}
		svc, err := build(cmd)
		if err != nil {
			return err
		}
		subject, err := svc.Authenticate(cmd.Context())
		if err != nil {
			return err
		}
		value, err := svc.ContinueDoerDraftSuccessorAs(cmd.Context(), subject, input.ID, input.ExpectedVersion, input.Expected, input.Charter)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	return []*cobra.Command{readiness, save, show, continuation}
}
