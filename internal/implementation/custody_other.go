//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package implementation

import (
	"errors"
	"os"
	"os/exec"
)

func lockWorkspace(string) (func(), error) {
	return nil, errors.New("workspace custody unsupported on this platform")
}
func configureProcess(*exec.Cmd) error {
	return errors.New("process group custody unsupported on this platform")
}
func killProcessGroup(*exec.Cmd) error { return nil }
func singleLink(os.FileInfo) bool      { return false }
