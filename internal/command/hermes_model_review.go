package command

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/spf13/cobra"
)

var (
	hermesReviewConfig   string
	hermesReviewSocket   string
	hermesReviewOrigin   string
	hermesReviewAgent    string
	hermesReviewProvider string
	hermesReviewModel    string
)

// This companion has no selection, credential or decision parameters. Its
// launcher owns foreground admission; neither launch nor peer UID authorizes.
func hermesModelReviewCmd(o *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "hermes-model-review", Hidden: true, Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		in, i := cmd.InOrStdin().(*os.File)
		out, j := cmd.OutOrStdout().(*os.File)
		diag, k := cmd.ErrOrStderr().(*os.File)
		if !i || !j || !k {
			return errors.New("protected_setup_unavailable")
		}
		x, e := in.Stat()
		y, f := out.Stat()
		z, g := diag.Stat()
		if e != nil || f != nil || g != nil || x.Mode()&os.ModeCharDevice == 0 || !os.SameFile(x, y) || !os.SameFile(x, z) {
			return errors.New("protected_setup_unavailable")
		}
		fg, err := unix.IoctlGetInt(int(in.Fd()), unix.TIOCGPGRP)
		if err != nil || fg != unix.Getpgrp() {
			return errors.New("protected_setup_unavailable")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Second)
		defer cancel()
		lookup := exec.CommandContext(ctx, "/usr/bin/loginctl", "show-session", "self", "--property=TTY", "--value")
		lookup.Env = []string{"LC_ALL=C"}
		raw, err := lookup.Output()
		name := strings.TrimSpace(string(raw))
		if err != nil || !regexp.MustCompile(`^(tty[0-9]+|pts/[0-9]+)$`).MatchString(name) {
			return errors.New("protected_setup_unavailable")
		}
		login, err := os.Stat("/dev/" + name)
		if err != nil || !os.SameFile(x, login) {
			return errors.New("protected_setup_unavailable")
		}
		if o.configFile != hermesReviewConfig {
			return errors.New("protected_setup_unavailable")
		}
		cfg, e := config.Load(o.configFile, nil)
		if e != nil || cfg.API.UnixSocket != hermesReviewSocket {
			return errors.New("protected_setup_unavailable")
		}
		// The deployment supplies its public origin at Serve launch, not in
		// this checkout config. Pin the HTTP Host/Origin; dial only its Unix socket.
		cfg.API.Console.Origin = hermesReviewOrigin
		if discardProtectedTerminalInput(in) != nil {
			return errors.New("protected_setup_unavailable")
		}
		return runManagerModelAdd(cmd.Context(), cmd, cfg, app.AgentModelAddInput{AgentID: hermesReviewAgent, Provider: hermesReviewProvider, Model: hermesReviewModel})
	}}
}
