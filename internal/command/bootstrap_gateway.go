package command

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

// prepareBootstrapGateway establishes an offline boundary before bootstrap
// inspects or opens authority stores. A socket is never permission to fall back
// to local stores, even when the listener is stale or unavailable.
func prepareBootstrapGateway(cmd *cobra.Command, configPath string, runner userservice.Runner, input *terminalInput, view *bootstrapPresentation) (bool, error) {
	inspection := config.Inspect(configPath)
	if inspection.State != config.StateValid || inspection.Config.API.UnixSocket == "" {
		return true, nil
	}
	socket := inspection.Config.API.UnixSocket
	info, err := os.Lstat(socket)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("control_plane_unavailable: inspect bootstrap transport: %w", err)
	}
	stat, owned := info.Sys().(*syscall.Stat_t)
	parent, parentErr := os.Lstat(filepath.Dir(socket))
	resolved, resolveErr := filepath.EvalSymlinks(socket)
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0077 != 0 || !owned || int(stat.Uid) != os.Geteuid() || parentErr != nil || !parent.IsDir() || parent.Mode().Perm()&0022 != 0 || resolveErr != nil || resolved != socket {
		return false, errors.New("control_plane_unavailable: unsafe bootstrap Unix transport; no socket was removed and no local stores were opened")
	}
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	plan, err := userservice.Preview(executable, configPath)
	if err != nil {
		return false, fmt.Errorf("bootstrap_gateway_recovery_required: cannot verify gateway; preserve state and stop its verified owner before rerunning 'aegis init': %w", err)
	}
	gateway := userservice.ObserveExactGateway(cmd.Context(), runner, plan)
	if gateway.State != userservice.GatewayHealthy {
		return false, fmt.Errorf("bootstrap_gateway_recovery_required: %s; run 'aegis gateway status'; stale, unavailable, or foreign transport must be repaired by its owner before rerunning 'aegis init'; no socket was removed and no local stores were opened", gateway.Reason)
	}
	approved, err := view.approve(cmd, input, bootstrapDecision{
		Title:          "Stop exact gateway to resume bootstrap",
		Recommendation: "Stop the verified Aegis gateway before resuming incomplete local setup.",
		Consequence:    "Temporarily interrupts gateway sessions. Preserves configuration, identities, approvals, models, and stores. Model binding, certification, registration, and activation still require their own approvals. If setup is declined or fails, the gateway remains stopped; rerun 'aegis init' to resume.",
		Details:        fmt.Sprintf("principal=%s; unit=%s; digest=%s; configuration=%s", plan.Principal, plan.UnitPath, plan.UnitDigest, plan.ConfigPath),
		DefaultDecline: true,
	})
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "Gateway recovery declined; no gateway or bootstrap state was changed.")
		return false, err
	}
	// Action revalidates the exact plan, loaded fragment, and full ExecStart both
	// before and after stopping, then waits for the observed inactive state.
	if _, err = userservice.Action(cmd.Context(), runner, plan, "stop", 20*time.Second); err != nil {
		return false, fmt.Errorf("bootstrap_gateway_stop_failed: state preserved; inspect 'aegis gateway status' before resuming: %w", err)
	}
	if _, err = os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		return false, errors.New("bootstrap_gateway_transport_remaining: gateway stopped but Unix transport remains or cannot be inspected; no socket was removed and no local stores were opened")
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Exact gateway stopped; resuming verified bootstrap artifacts without resetting state. Gateway activation is not automatic if setup is declined or fails.")
	return true, nil
}
