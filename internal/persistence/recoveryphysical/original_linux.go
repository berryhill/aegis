package recoveryphysical

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/sys/unix"
)

// Original engine bytes are never opened by Badger or chmodded. This narrow
// admission is ONLY for authenticated offline recovery under its writer lease.
// Like the existing custody model, it trusts the current UID and root, not
// hostile processes with those identities or historical permission changes.
var originalEngineName = regexp.MustCompile(`^(LOCK|MANIFEST|KEYREGISTRY|DISCARD|[0-9]{6,}\.sst|[0-9]{6,}\.vlog|[0-9]{5,}\.mem)$`)
var umaskEngineName = regexp.MustCompile(`^(LOCK|DISCARD|[0-9]{6,}\.vlog|[0-9]{5,}\.mem)$`)

func noOriginalACL(fd int) error {
	for _, name := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		_, err := unix.Fgetxattr(fd, name, nil)
		if err != unix.ENODATA && err != unix.ENOTSUP {
			return errors.New("unqualified original recovery ACL")
		}
	}
	return nil
}

type originalDirectoryPin struct {
	files []*os.File
	stats []unix.Stat_t
}

func (p *originalDirectoryPin) close() {
	for _, f := range p.files {
		_ = f.Close()
	}
}
func (p *originalDirectoryPin) check() error {
	for i, f := range p.files {
		var now unix.Stat_t
		if unix.Fstat(int(f.Fd()), &now) != nil || now.Dev != p.stats[i].Dev || now.Ino != p.stats[i].Ino || now.Mode != p.stats[i].Mode || now.Uid != p.stats[i].Uid || now.Gid != p.stats[i].Gid || noOriginalACL(int(f.Fd())) != nil {
			return errors.New("original recovery ancestry changed")
		}
		if i > 0 {
			var named unix.Stat_t
			if unix.Fstatat(int(p.files[i-1].Fd()), filepath.Base(f.Name()), &named, unix.AT_SYMLINK_NOFOLLOW) != nil || named.Dev != now.Dev || named.Ino != now.Ino {
				return errors.New("original recovery ancestry replaced")
			}
		}
	}
	return nil
}
func pinOriginalDirectory(path string) (p *originalDirectoryPin, err error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("invalid original recovery path")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 256 {
		return nil, errors.New("original recovery ancestry bound")
	}
	p = &originalDirectoryPin{}
	pinned := p
	defer func() {
		if err != nil {
			pinned.close()
		}
	}()
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	p.files = append(p.files, os.NewFile(uintptr(fd), "/"))
	private := false
	for i := 0; ; i++ {
		var st unix.Stat_t
		if err = unix.Fstat(fd, &st); err != nil {
			return p, err
		}
		if err = noOriginalACL(fd); err != nil {
			return p, err
		}
		if (st.Uid != 0 && int(st.Uid) != os.Geteuid()) || (!private && st.Mode&0022 != 0 && !(st.Uid == 0 && st.Mode&unix.S_ISVTX != 0)) {
			return p, errors.New("unsafe original recovery ancestry")
		}
		if int(st.Uid) == os.Geteuid() && st.Mode&0077 == 0 {
			private = true
		}
		p.stats = append(p.stats, st)
		if i == len(parts) {
			if int(st.Uid) != os.Geteuid() || st.Mode&07777 != 0700 {
				return p, errors.New("original engine custody is not private")
			}
			return p, p.check()
		}
		fd, err = unix.Openat(fd, parts[i], unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return p, err
		}
		p.files = append(p.files, os.NewFile(uintptr(fd), parts[i]))
	}
}

// OriginalCopy admits the pinned Badger version's group-writable log/lock modes
// only within verified private custody. Controls and candidates use Copy instead.
func OriginalCopy(ctx context.Context, source, destination string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	name := filepath.Base(source)
	if !originalEngineName.MatchString(name) {
		return "", errors.New("unknown original engine file")
	}
	p, err := pinOriginalDirectory(filepath.Dir(source))
	if err != nil {
		return "", err
	}
	defer p.close()
	parent := int(p.files[len(p.files)-1].Fd())
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	f := os.NewFile(uintptr(fd), source)
	defer f.Close()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return "", err
	}
	mode := st.Mode & 07777
	allowed := mode == 0600 || mode == 0644 || ((mode == 0664 || mode == 0660) && umaskEngineName.MatchString(name))
	if st.Mode&unix.S_IFMT != unix.S_IFREG || int(st.Uid) != os.Geteuid() || st.Nlink != 1 || st.Size < 0 || st.Size > recoveryByteLimit || !allowed {
		return "", errors.New("unsafe original engine file")
	}
	if err = noOriginalACL(fd); err != nil {
		return "", err
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	sum, err := copyOpened(ctx, f, info, destination)
	if err != nil {
		return "", err
	}
	var after, named unix.Stat_t
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return "", errors.New("original engine identity unavailable")
	}
	// Access time may change on reading; custody and content metadata may not.
	if after.Dev != st.Dev || after.Ino != st.Ino || after.Mode != st.Mode || after.Uid != st.Uid || after.Gid != st.Gid || after.Nlink != 1 || after.Size != st.Size || after.Mtim != st.Mtim || after.Ctim != st.Ctim || named.Dev != st.Dev || named.Ino != st.Ino || noOriginalACL(fd) != nil {
		return "", errors.New("original engine changed during snapshot")
	}
	if err = p.check(); err != nil {
		return "", err
	}
	return sum, ctx.Err()
}

// OriginalDirectory is deliberately separate from strict evidence/candidate
// inventory. Its digest remains byte/name-compatible with Directory receipts.
func OriginalDirectory(ctx context.Context, source, dest string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	p, err := pinOriginalDirectory(source)
	if err != nil {
		return "", err
	}
	defer p.close()
	sum, err := directory(ctx, source, dest, OriginalCopy)
	if err != nil {
		return "", err
	}
	if err = p.check(); err != nil {
		return "", err
	}
	return sum, nil
}
