package command

import (
	"errors"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/initialize"
	"github.com/spf13/cobra"
)

func authorityRecoveryCmd(o *rootOptions, profile ExecutionProfile) *cobra.Command {
	group := &cobra.Command{Use: "authority", Short: "Offline operational authority maintenance"}
	group.AddCommand(&cobra.Command{Use: "recover-interrupted", Short: "Authenticate and validate one interrupted-open authority generation; preserve evidence", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if o.target != "" {
			return errors.New("interrupted authority recovery is offline only")
		}
		cfg, err := config.Load(o.configFile, nil)
		if err != nil {
			return err
		}
		if cmd.Flags().Changed("state-dir") || cmd.InheritedFlags().Changed("state-dir") {
			return errors.New("authority recovery requires the configured state directory; --state-dir overrides are forbidden")
		}
		if err = validateConfiguredPathsProfile(profile, map[string]string{"state": cfg.StateDir}); err != nil {
			return err
		}
		provider, err := passphraseProvider(cmd)
		if err != nil {
			return err
		}
		password, err := provider.Acquire(cmd.Context(), AuthorityPassphraseRequest{Intent: PrincipalPasswordAuthenticate, Input: cmd.InOrStdin(), Diagnostic: cmd.ErrOrStderr()})
		if err != nil {
			return err
		}
		defer clear(password)
		result, err := initialize.RecoverInterruptedAuthority(cmd.Context(), cfg, password)
		// Emit retained evidence custody even when verification refuses recovery.
		if result.EvidencePath != "" {
			if outErr := output(cmd, result); outErr != nil {
				return errors.Join(err, outErr)
			}
		}
		return err
	}})
	return group
}
