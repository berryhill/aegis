package command

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/berryhill/aegis/internal/config"
	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

// A source coordinator may repair an explicitly bound installed production
// release. Neither executable nor configuration is inferred from foreign bytes.
func addGatewayRecovery(root *cobra.Command, deps Dependencies, options *rootOptions) {
	var executable string
	var apply bool
	cmd := &cobra.Command{Use: "recover", Short: "Preview or recover a stopped foreign user unit; never start it", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		canonical := filepath.Join(home, ".aegis", "aegis.yaml")
		if options.configFile != "" && options.configFile != canonical {
			return errors.New("recovery requires exact canonical production configuration")
		}
		if !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
			return errors.New("--installed-executable must bind one absolute installed release")
		}
		installedInfo, installedDigest, err := userservice.RecoveryExecutable(executable)
		if err != nil {
			return err
		}
		coordinator := selfupdate.New("dev")
		if _, err = coordinator.ForInstalled(executable); err != nil {
			return fmt.Errorf("installed release identity: %w", err)
		}
		plan, err := userservice.Preview(executable, canonical)
		if err != nil {
			return err
		}
		if plan.UnitPath != filepath.Join(home, ".config", "systemd", "user", userservice.UnitName) {
			return errors.New("recovery requires canonical production user unit path; XDG override denied")
		}
		cfg, err := config.Load(canonical, nil)
		if err != nil {
			return err
		}
		if cfg.StateDir != filepath.Join(home, ".aegis", "state") {
			return errors.New("recovery requires canonical production state")
		}
		preview, err := userservice.PreviewRecovery(cmd.Context(), deps.UserService, plan)
		if err != nil {
			return err
		}
		if !apply {
			return output(cmd, preview)
		}
		if !deps.IsTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return errors.New("recovery requires an interactive protected principal approval")
		}
		data, err := os.ReadFile(canonical)
		if err != nil {
			return err
		}
		journey := &installedJourney{cmd: cmd, runner: deps.UserService, executable: executable, configPath: canonical, plan: plan, cfg: cfg, configDigest: sha256.Sum256(data)}
		defer func() { clear(journey.password) }()
		if err = journey.admit(cmd.Context()); err != nil {
			return err
		}
		if err = output(cmd, preview); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Type %s to preserve foreign bytes and restore only this stopped unit: ", preview.Confirmation)
		answer, eof, err := newTerminalInput(cmd.InOrStdin()).ReadLine(cmd.Context(), 256)
		if err != nil {
			return err
		}
		if eof || answer != preview.Confirmation {
			return errors.New("recovery confirmation rejected; no writes performed")
		}
		if err = journey.admit(cmd.Context()); err != nil {
			return err
		}
		afterInfo, afterDigest, err := userservice.RecoveryExecutable(executable)
		if err != nil {
			return err
		}
		if _, err = coordinator.ForInstalled(executable); err != nil {
			return err
		}
		if !os.SameFile(installedInfo, afterInfo) || installedDigest != afterDigest {
			return errors.New("installed executable changed after recovery preview")
		}
		if err = userservice.ApplyRecovery(cmd.Context(), deps.UserService, preview, answer); err != nil {
			return err
		}
		return output(cmd, map[string]any{"status": "unit_recovered", "loaded_identity_verified": true, "ready": false, "audit_current": false, "evidence_path": preview.EvidencePath, "next_step": "aegis update --installed --config " + canonical})
	}}
	cmd.Flags().StringVar(&executable, "installed-executable", "", "exact existing production release executable (never overwritten by recovery)")
	cmd.Flags().BoolVar(&apply, "apply", false, "authenticate and confirm exact recovery; default is preview only")
	for _, c := range root.Commands() {
		if c.Name() == "gateway" {
			c.AddCommand(cmd)
			return
		}
	}
}
