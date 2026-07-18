//go:build darwin

package command

import "golang.org/x/sys/unix"

func disableTerminalEcho(fd int) (func() error, error) {
	state, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil, err
	}
	noEcho := *state
	noEcho.Lflag &^= unix.ECHO
	if err = unix.IoctlSetTermios(fd, unix.TIOCSETA, &noEcho); err != nil {
		return nil, err
	}
	return func() error { return unix.IoctlSetTermios(fd, unix.TIOCSETA, state) }, nil
}
