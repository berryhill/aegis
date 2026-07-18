//go:build !linux

package broker

import (
	"errors"
	"net"
	"os"
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

func (listener *peerListener) Accept() (net.Conn, error) {
	return nil, errors.New("Unix peer credentials are unsupported on this platform")
}
func socketOwner(os.FileInfo) (uint32, uint32, bool) { return 0, 0, false }
