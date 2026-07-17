//go:build !linux

package api

import (
	"context"
	"net"
)

func unixPeerContext(ctx context.Context, _ net.Conn) context.Context {
	// Principal API identity requires Linux SO_PEERCRED. Other platforms fail
	// closed because no peer UID is attached to the request context.
	return ctx
}
