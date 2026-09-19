//go:build !unix

package testprocess

import "os/exec"

func own(command *exec.Cmd) {}
func cleanup(command *exec.Cmd) {
	if command.Process != nil {
		_ = command.Process.Kill()
	}
}
