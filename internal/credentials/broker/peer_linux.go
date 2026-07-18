//go:build linux

package broker

import (
	"errors"
	"net"
	"os"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

type authenticatedConn struct {
	net.Conn
	peer Peer
}

type peerListener struct {
	net.Listener
	uid uint32
	gid uint32
	sem chan struct{}
}

type limitedConn struct {
	net.Conn
	sem  chan struct{}
	once sync.Once
}

func (connection *limitedConn) Close() error {
	err := connection.Conn.Close()
	connection.once.Do(func() { <-connection.sem })
	return err
}

func (listener *peerListener) Accept() (net.Conn, error) {
	for {
		connection, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		unixConnection, ok := connection.(*net.UnixConn)
		if !ok {
			_ = connection.Close()
			return nil, errors.New("broker accepted a non-Unix connection")
		}
		raw, err := unixConnection.SyscallConn()
		if err != nil {
			_ = connection.Close()
			continue
		}
		var credential *unix.Ucred
		var socketErr error
		if err = raw.Control(func(fd uintptr) {
			credential, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		}); err != nil || socketErr != nil || credential == nil {
			_ = connection.Close()
			continue
		}
		// This check occurs in Accept, before net/http can parse headers or
		// expose a request body to an action handler.
		if credential.Uid != listener.uid || credential.Gid != listener.gid {
			_ = connection.Close()
			continue
		}
		select {
		case listener.sem <- struct{}{}:
			return &authenticatedConn{Conn: &limitedConn{Conn: connection, sem: listener.sem}, peer: Peer{PID: credential.Pid, UID: credential.Uid, GID: credential.Gid}}, nil
		default:
			_ = connection.Close()
		}
	}
}

func socketOwner(info os.FileInfo) (uint32, uint32, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return stat.Uid, stat.Gid, true
}
