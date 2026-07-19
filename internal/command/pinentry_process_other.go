//go:build !unix

package command

import "os/exec"

func configureProtectedProcess(*exec.Cmd) {}

func terminateProtectedProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
