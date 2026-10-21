package command

import (
	"errors"
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
		value, err := svc.ReadDoerReadinessAs(cmd.Context(), subject, input)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	readiness.Flags().Bool("probe", false, "Explicitly probe the bounded local helper protocol")
	save := &cobra.Command{Use: "task-save FILE", Short: "Retain a Doer proposal using its exact version guard", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
	show := &cobra.Command{Use: "task-show ID", Short: "Read one retained Doer proposal", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
	continuation := &cobra.Command{Use: "task-continue FILE", Short: "Rebind a retained proposal to an already approved successor", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
	review := &cobra.Command{Use: "setup-review FILE", Short: "Review a credential-only successor through the owning service", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("setup review requires --target owning-service transport")
	}}
	decision := &cobra.Command{Use: "setup-decide FILE", Short: "Reject a setup review; approval requires independent protected human confirmation", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("setup decision requires --target owning-service transport")
	}}
	protected := &cobra.Command{Use: "setup-approve ID", Short: "Authenticate and review exact retained setup in a protected terminal (no browser)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("protected setup requires --target owning-service transport")
	}}
	protected.Flags().Uint64("expected-version", 0, "exact retained task version")
	protected.Flags().String("action", "successor", "independent exact scope: successor, provision, or host")
	invoke := &cobra.Command{Use: "setup-protected FILE", Short: "Request independent protected desktop setup review from the owning service", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("protected setup invocation requires --target owning-service transport")
	}}
	browser := &cobra.Command{Use: "setup-browser FILE", Short: "Open independent browser setup for the exact retained task (no desktop required)", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("browser setup requires --target owning-service transport")
	}}
	execute := &cobra.Command{Use: "task-execute FILE", Short: "Create or resume one retained task and execute in the owning service", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return errors.New("task execution requires --target owning-service transport")
	}}
	legacySave, legacyShow, legacyContinue := *save, *show, *continuation
	legacySave.Use, legacyShow.Use, legacyContinue.Use = "draft-save FILE", "draft-show ID", "draft-continue FILE"
	legacySave.Hidden, legacyShow.Hidden, legacyContinue.Hidden = true, true, true
	return []*cobra.Command{readiness, execute, save, show, continuation, &legacySave, &legacyShow, &legacyContinue, review, decision, protected, invoke, browser}
}
