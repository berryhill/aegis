//go:build !unix

package command

import (
	"io"
	"os/exec"
)

func protectedInputTTY(io.Reader) string { return "" }

func configureProtectedProcess(*exec.Cmd) {}

func terminateProtectedProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
