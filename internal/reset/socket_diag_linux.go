//go:build linux

package reset

import (
	"encoding/binary"
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// Linux UAPI linux/unix_diag.h: request=24 bytes, reply=16 bytes,
// UNIX_DIAG_VFS=1 contains two native-endian u32s (inode, kernel dev_t).
// net/unix/diag.c exports s_dev directly, NOT userspace stat's dev encoding.
// Names are deliberately not authority: bind accepts relative/aliased paths.
func requireUnboundSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	id, err := socketIdentity(info)
	if err != nil {
		return err
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Sec: 1}); err != nil {
		return err
	}
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return err
	}
	b := make([]byte, 40)
	order := binary.NativeEndian
	order.PutUint32(b[0:4], uint32(len(b)))
	order.PutUint16(b[4:6], unix.SOCK_DIAG_BY_FAMILY)
	order.PutUint16(b[6:8], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
	order.PutUint32(b[8:12], 1)
	b[16] = unix.AF_UNIX
	order.PutUint32(b[20:24], 0xffffffff)
	order.PutUint32(b[28:32], 2) // all states; UDIAG_SHOW_VFS
	if err := unix.Sendto(fd, b, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return err
	}
	buf := make([]byte, 1<<20)
	for {
		n, _, flags, from, err := unix.Recvmsg(fd, buf, nil, 0)
		if err != nil {
			return err
		}
		sender, ok := from.(*unix.SockaddrNetlink)
		if !ok || sender.Pid != 0 || flags&unix.MSG_TRUNC != 0 {
			return errors.New("unverified socket diagnostic source or truncated inventory")
		}
		messages, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			return errors.New("empty socket inventory")
		}
		for _, m := range messages {
			if m.Header.Seq != 1 || m.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
				return errors.New("interrupted socket inventory")
			}
			switch m.Header.Type {
			case unix.NLMSG_DONE:
				if len(m.Data) < 4 || order.Uint32(m.Data[:4]) != 0 {
					return errors.New("socket inventory failed")
				}
				return nil
			case unix.SOCK_DIAG_BY_FAMILY:
				if len(m.Data) < 16 || m.Data[0] != unix.AF_UNIX {
					return errors.New("malformed socket diagnostic")
				}
				for attrs := m.Data[16:]; len(attrs) > 0; {
					if len(attrs) < 4 {
						return errors.New("short socket attribute")
					}
					size := int(order.Uint16(attrs[:2]))
					aligned := (size + 3) &^ 3
					if size < 4 || aligned > len(attrs) {
						return errors.New("malformed socket attribute")
					}
					if order.Uint16(attrs[2:4]) == 1 {
						if size != 12 {
							return errors.New("malformed VFS identity")
						}
						dev := order.Uint32(attrs[8:12])
						ino := order.Uint32(attrs[4:8])
						// UAPI truncates inode to u32: conservatively deny collisions as well.
						if ino == uint32(id.Inode) && dev>>20 == unix.Major(id.Device) && dev&0xfffff == unix.Minor(id.Device) {
							return errors.New("transport is still bound in the kernel; stop its verified owner")
						}
					}
					attrs = attrs[aligned:]
				}
			default:
				return errors.New("socket diagnostic inventory unavailable")
			}
		}
	}
}
