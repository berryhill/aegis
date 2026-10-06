package command

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/spf13/cobra"
)

// The owning service launches this pinned product binary. The skill never
// captures a password, cookie, CSRF value, native decision or review receipt.
func NewDoerSetupCompanionCommand(configPath func() string) *cobra.Command {
	var socket, origin, id, action string
	var version uint64
	c := &cobra.Command{Use: "doer-setup-companion", Hidden: true, Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().StringVar(&socket, "socket", "", "owning protected Unix socket")
	c.Flags().StringVar(&origin, "origin", "", "owning origin")
	c.Flags().StringVar(&id, "draft-id", "", "exact retained draft")
	c.Flags().Uint64Var(&version, "expected-version", 0, "exact retained version")
	c.Flags().StringVar(&action, "action", "", "exact setup scope")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		status := "policy_rejected"
		for _, flag := range []string{"target", "state-dir", "hermes-executable", "runtime", "pinentry-executable", "update"} {
			if cmd.Flags().Changed(flag) || cmd.InheritedFlags().Changed(flag) {
				return output(cmd, map[string]string{"status": status})
			}
		}
		path := ""
		if configPath != nil {
			path = configPath()
		}
		cfg, err := config.Load(path, nil)
		if err != nil || cfg.API.UnixSocket != socket || cfg.API.Console.Origin != origin || !companionID.MatchString(id) || version == 0 || (action != "successor" && action != "host" && action != "provision") {
			return output(cmd, map[string]string{"status": status})
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		bearer, e := readCompanionBearer(ctx, 3)
		if e != nil {
			return output(cmd, map[string]string{"status": "transport_custody_rejected"})
		}
		matched := subtle.ConstantTimeCompare(bearer, []byte(cfg.API.Token)) == 1
		wipeSecret(bearer)
		if !matched {
			return output(cmd, map[string]string{"status": "transport_custody_rejected"})
		}
		// Product-launched approval uses only independent protected desktop input.
		// Never borrow a controlling terminal, model stdin or a tool-created PTY.
		acquire := acquireDoerSetupPassword
		confirm := func(ctx context.Context, review app.DoerProtectedSetupReview) string {
			switch status := newNativeConfirmation().confirm(ctx, doerProtectedReviewDescription(review)); status {
			case "confirmed":
				return "approve"
			case "denied":
				return "reject"
			default:
				return status
			}
		}
		err = executeDoerProtectedSetup(ctx, cfg, app.DoerProtectedSetupInput{ID: id, ExpectedVersion: version, Action: action}, acquire, confirm, func(app.DoerProtectedSetupResult) error { status = "approved"; return nil })
		if err != nil {
			status = "unknown_response"
			for _, safe := range []string{"cancelled", "denied", "timeout", "unavailable", "outcome_unknown", "response_unverified", "policy_rejected"} {
				if strings.HasPrefix(err.Error(), "protected_setup_"+safe) {
					status = safe
					break
				}
			}
			if IsPassphraseError(err, PassphraseCancelled) {
				status = "cancelled"
			}
			if IsPassphraseError(err, PassphraseTimeout) {
				status = "timeout"
			}
			if errors.Is(err, context.Canceled) {
				status = "cancelled"
			}
		}
		return output(cmd, map[string]string{"status": status})
	}
	return c
}
