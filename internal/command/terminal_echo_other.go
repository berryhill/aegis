//go:build !linux && !darwin

package command

import "golang.org/x/term"

func disableTerminalEcho(fd int) (func() error, error) {
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error { return term.Restore(fd, state) }, nil
}
