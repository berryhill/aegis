//go:build unix

package command

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"
)

func protectedInputTTY(input io.Reader) string {
	file, ok := input.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return ""
	}
	// File.Name is caller metadata (os.Stdin is normally /dev/stdin). On
	// Linux resolve the actual descriptor first; verify device identity below.
	path := file.Name()
	if resolved, err := os.Readlink("/proc/self/fd/" + strconv.FormatUint(uint64(file.Fd()), 10)); err == nil {
		path = resolved
	}
	if path != "/dev/tty" && !strings.HasPrefix(path, "/dev/pts/") && !strings.HasPrefix(path, "/dev/tty") {
		return ""
	}
	if filepath.Clean(path) != path || safePinentryOption(path) == "" {
		return ""
	}
	fdInfo, err := file.Stat()
	if err != nil || fdInfo.Mode()&os.ModeCharDevice == 0 {
		return ""
	}
	pathInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(fdInfo, pathInfo) {
		return ""
	}
	return path
}

func configureProtectedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateProtectedProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}
