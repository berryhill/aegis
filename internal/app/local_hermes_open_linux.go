//go:build linux

package app

import "golang.org/x/sys/unix"

func localHermesDirectoryOpenFlags() int {
	return unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
}

func localHermesMarkerOpenFlags() int {
	return unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW
}
