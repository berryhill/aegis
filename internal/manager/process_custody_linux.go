//go:build linux

package manager

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// ProcessCustody retains a pidfd for one exact process. The pidfd prevents PID
// reuse from changing the process to which this authority is bound.
type ProcessCustody struct {
	mu     sync.RWMutex
	pid    int
	pidfd  int
	bootID string
}

func AcquireProcessCustody(pid int) (*ProcessCustody, error) {
	if pid <= 0 {
		return nil, errors.New("invalid process custody PID")
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, fmt.Errorf("acquire Hermes pidfd custody: %w", err)
	}
	bootID, err := currentBootID()
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("acquire host boot identity for Hermes custody: %w", err)
	}
	return &ProcessCustody{pid: pid, pidfd: fd, bootID: bootID}, nil
}

func (c *ProcessCustody) Alive() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pidfd >= 0 && sameBoot(c.bootID) && unix.PidfdSendSignal(c.pidfd, 0, nil, 0) == nil
}

func (c *ProcessCustody) Signal(signal syscall.Signal) error {
	if c == nil {
		return errors.New("missing process custody")
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.pidfd < 0 {
		return errors.New("process custody is closed")
	}
	if !sameBoot(c.bootID) {
		return errors.New("process custody belongs to another host boot")
	}
	return unix.PidfdSendSignal(c.pidfd, unix.Signal(signal), nil, 0)
}

// OwnsTCPConnection verifies that the accepted loopback connection has a
// socket descriptor in the exact pidfd-custodied process. This makes the
// parser-compatible API-key header non-authoritative.
func (c *ProcessCustody) OwnsTCPConnection(remote, local net.Addr) bool {
	if c == nil || !c.Alive() {
		return false
	}
	remotePort, ok := tcpPort(remote)
	if !ok {
		return false
	}
	localPort, ok := tcpPort(local)
	if !ok {
		return false
	}

	c.mu.RLock()
	pid := c.pid
	c.mu.RUnlock()
	inodes, err := processSocketInodes(pid)
	if err != nil || len(inodes) == 0 {
		return false
	}
	for _, table := range []string{"tcp", "tcp6"} {
		data, readErr := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "net", table))
		if readErr != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "01" || !inodes[fields[9]] {
				continue
			}
			childPort, childOK := procPort(fields[1])
			proxyPort, proxyOK := procPort(fields[2])
			if childOK && proxyOK && childPort == remotePort && proxyPort == localPort {
				return true
			}
		}
	}
	return false
}

func (c *ProcessCustody) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	fd := c.pidfd
	c.pidfd = -1
	c.mu.Unlock()
	if fd < 0 {
		return nil
	}
	return unix.Close(fd)
}

func tcpPort(address net.Addr) (int, bool) {
	if address == nil {
		return 0, false
	}
	_, rawPort, err := net.SplitHostPort(address.String())
	if err != nil {
		return 0, false
	}
	port, err := strconv.Atoi(rawPort)
	return port, err == nil && port > 0
}

func procPort(endpoint string) (int, bool) {
	separator := strings.LastIndexByte(endpoint, ':')
	if separator < 0 || separator == len(endpoint)-1 {
		return 0, false
	}
	value, err := strconv.ParseUint(endpoint[separator+1:], 16, 16)
	return int(value), err == nil && value > 0
}

func processSocketInodes(pid int) (map[string]bool, error) {
	entries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(pid), "fd"))
	if err != nil {
		return nil, err
	}
	inodes := make(map[string]bool)
	for _, entry := range entries {
		target, readErr := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "fd", entry.Name()))
		if readErr == nil && strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
			inodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = true
		}
	}
	return inodes, nil
}

func currentBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	bootID := strings.TrimSpace(string(data))
	if bootID == "" {
		return "", errors.New("empty host boot identity")
	}
	return bootID, nil
}

func sameBoot(expected string) bool {
	bootID, err := currentBootID()
	return err == nil && expected != "" && bootID == expected
}
