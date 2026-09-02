//go:build !linux

package app

import "golang.org/x/sys/unix"

func localHermesDirectoryOpenFlags() int {
	return unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
}

func localHermesMarkerOpenFlags() int {
	return unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
}
