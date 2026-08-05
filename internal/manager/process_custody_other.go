//go:build !linux

package manager

import (
	"errors"
	"net"
	"syscall"
)

type ProcessCustody struct{}

func AcquireProcessCustody(int) (*ProcessCustody, error) {
	return nil, errors.New("Hermes pidfd process custody requires Linux")
}

func (*ProcessCustody) Alive() bool { return false }
func (*ProcessCustody) Signal(syscall.Signal) error {
	return errors.New("Hermes pidfd process custody requires Linux")
}
func (*ProcessCustody) OwnsTCPConnection(net.Addr, net.Addr) bool { return false }
func (*ProcessCustody) Close() error                              { return nil }
