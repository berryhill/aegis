package hermes

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// runBoundedRuntime owns the whole child group and drains bounded private output.
// No ambient profile, Python, proxy or PM override reaches either bootstrap or code.
func runBoundedRuntime(ctx context.Context, launch gatewayLaunch, home, scratch string, limit int, files ...*os.File) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, -1, err
	}
	if err := launch.fresh(); err != nil {
		return nil, -1, err
	}
	cmd := exec.CommandContext(ctx, launch.argv[0], launch.argv[1:]...)
	cmd.Env = []string{"HOME=" + home, "HERMES_HOME=" + home, "TMPDIR=" + scratch, "PATH=/usr/bin:/bin", "LANG=C.UTF-8", "PYTHONDONTWRITEBYTECODE=1", "HERMES_SKIP_VERSION_CHECK=1", "HERMES_ENABLE_PROJECT_PLUGINS=false", "HERMES_DISABLE_LAZY_INSTALLS=1"}
	cmd.Dir = scratch
	cmd.ExtraFiles = files
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output commandOutput
	output.limit = limit
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := prepareAttemptProcess(); err != nil {
		return nil, -1, err
	}
	if err := cmd.Start(); err != nil {
		return nil, -1, errGatewayCommand
	}
	exited := make(chan error, 1)
	stop := watchAttemptProcess(cmd, exited)
	select {
	case <-ctx.Done():
	case <-exited:
	}
	stop()
	if err := stopOwnedAttemptProcess(cmd, io.NopCloser(strings.NewReader(""))); err != nil {
		output.discard()
		return nil, -1, err
	}
	if err := ctx.Err(); err != nil {
		output.discard()
		return nil, -1, err
	}
	if output.overflow || cmd.ProcessState == nil {
		output.discard()
		return nil, -1, errGatewayCommand
	}
	return output.Bytes(), cmd.ProcessState.ExitCode(), nil
}
