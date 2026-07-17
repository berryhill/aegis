//go:build linux

package api

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func unixPeerContext(ctx context.Context, connection net.Conn) context.Context {
	raw, ok := connection.(syscall.Conn)
	if !ok {
		return ctx
	}
	rawConn, err := raw.SyscallConn()
	if err != nil {
		return ctx
	}
	var credential *unix.Ucred
	_ = rawConn.Control(func(fd uintptr) {
		credential, err = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
	if err != nil || credential == nil {
		return ctx
	}
	return context.WithValue(ctx, peerUIDKey{}, credential.Uid)
}
