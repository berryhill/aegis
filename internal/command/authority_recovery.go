package command

import (
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/initialize"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/spf13/cobra"
)

func authorityRecoveryCmd(o *rootOptions, profile ExecutionProfile) *cobra.Command {
	var quarantineOrphan bool
	group := &cobra.Command{Use: "authority", Short: "Offline operational authority maintenance"}
	recoverCommand := &cobra.Command{Use: "recover-interrupted", Short: "Authenticate and verify interrupted authority and fleet state; preserve evidence", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
		var transport *recoveryTransport
		if quarantineOrphan {
			transport, err = prepareRecoveryTransport(cmd.Context(), o.configFile, cfg)
			if err != nil {
				return err
			}
		}
		provider, err := passphraseProvider(cmd)
		if err != nil {
			return err
		}
		if _, err = fmt.Fprintln(cmd.ErrOrStderr(), "Authenticate to verify this instance's interrupted authority and fleet state. Original evidence is preserved; recovery does not renew runtime grants or replay unknown effects."); err != nil {
			return err
		}
		password, err := provider.Acquire(cmd.Context(), AuthorityPassphraseRequest{Intent: PrincipalPasswordAuthenticate, Input: cmd.InOrStdin(), Diagnostic: cmd.ErrOrStderr()})
		if err != nil {
			return err
		}
		defer clear(password)
		var result authoritybadger.InterruptedRecovery
		if transport == nil {
			result, err = initialize.RecoverInterruptedInstance(cmd.Context(), cfg, password)
		} else {
			result, err = transport.recover(cmd.Context(), password)
		}
		quarantined := ""
		if transport != nil {
			quarantined = transport.review.EvidencePath()
		}
		// Emit retained evidence custody even when verification refuses recovery.
		if result.EvidencePath != "" || quarantined != "" {
			var value any = result
			if transport != nil {
				value = struct {
					authoritybadger.InterruptedRecovery
					QuarantinedTransport string `json:"quarantined_transport,omitempty"`
				}{result, quarantined}
			}
			if outErr := output(cmd, value); outErr != nil {
				return errors.Join(err, outErr)
			}
		}
		return err
	}}
	recoverCommand.Flags().BoolVar(&quarantineOrphan, "quarantine-orphan-transport", false, "After principal authentication, preserve a kernel-proven unbound orphan socket while holding startup exclusion through recovery")
	group.AddCommand(recoverCommand)
	return group
}
