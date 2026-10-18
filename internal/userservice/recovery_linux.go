package userservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/sys/unix"
)

// RecoveryPlan binds the foreign inode and bytes to one deterministic unit.
// It confers no authentication; the command coordinator owns that boundary.
type RecoveryPlan struct {
	Desired       Plan   `json:"desired"`
	ForeignDigest string `json:"foreign_digest"`
	Device        uint64 `json:"device"`
	Inode         uint64 `json:"inode"`
	EvidencePath  string `json:"evidence_path"`
	Confirmation  string `json:"confirmation"`
	foreign       []byte
}

// recoveryDirectory pins no-symlink ancestry. A private owned search barrier
// permits writable descendants, but never foreign ownership or POSIX ACLs.
func recoveryDirectory(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrForeignUnit
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	private := false
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return nil, e
		}
		fd = next
		var st unix.Stat_t
		if e = unix.Fstat(fd, &st); e != nil {
			unix.Close(fd)
			return nil, e
		}
		if e = recoveryNoACL(fd); e != nil {
			unix.Close(fd)
			return nil, e
		}
		if (st.Uid != 0 && int(st.Uid) != os.Geteuid()) || (!private && st.Mode&0022 != 0 && !(st.Uid == 0 && st.Mode&unix.S_ISVTX != 0)) {
			unix.Close(fd)
			return nil, fmt.Errorf("unsafe recovery ancestor %s: %w", part, ErrForeignUnit)
		}
		if int(st.Uid) == os.Geteuid() && st.Mode&0011 == 0 {
			private = true
		}
	}
	return os.NewFile(uintptr(fd), path), nil
}

// Any extended access/default ACL is an explicit unsupported recovery gate.
// ENOTSUP means this filesystem does not implement POSIX ACLs; other failures deny.
func recoveryNoACL(fd int) error {
	for _, name := range []string{"system.posix_acl_access", "system.posix_acl_default"} {
		n, e := unix.Fgetxattr(fd, name, nil)
		if e == unix.ENODATA || e == unix.ENOTSUP {
			continue
		}
		if e != nil {
			return fmt.Errorf("cannot verify recovery ACL: %w", e)
		}
		if n > 0 {
			return errors.New("recovery denies POSIX ACL ancestry or files")
		}
	}
	return nil
}

// RecoveryExecutable snapshots a regular, owned, single-link executable through
// a pinned no-symlink directory after checking access ancestry and POSIX ACLs.
func RecoveryExecutable(path string) (os.FileInfo, [32]byte, error) {
	var digest [32]byte
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, digest, ErrForeignUnit
	}
	dir, e := recoveryDirectory(filepath.Dir(path))
	if e != nil {
		return nil, digest, e
	}
	defer dir.Close()
	fd, e := unix.Openat(int(dir.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, digest, e
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if e = unix.Fstat(fd, &st); e != nil {
		return nil, digest, e
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || int(st.Uid) != os.Geteuid() || st.Nlink != 1 || st.Mode&0022 != 0 || st.Mode&0111 == 0 {
		return nil, digest, ErrForeignUnit
	}
	if e = recoveryNoACL(fd); e != nil {
		return nil, digest, e
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return nil, digest, e
	}
	copy(digest[:], h.Sum(nil))
	info, e := f.Stat()
	return info, digest, e
}
func recoveryNoOverrides(ctx context.Context, r Runner) error {
	if r == nil {
		return errors.New("service manager unavailable")
	}
	b, e := r.Output(ctx, "show", UnitName, "--property", "DropInPaths", "--value")
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(b)) != "" {
		return errors.New("recovery denies loaded systemd DropInPaths overrides")
	}
	return nil
}
func recoverySnapshot(dir *os.File, name string) ([]byte, unix.Stat_t, error) {
	var st unix.Stat_t
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, st, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	if err = recoveryNoACL(fd); err != nil {
		return nil, st, err
	}
	if err = unix.Fstat(fd, &st); err != nil {
		return nil, st, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || int(st.Uid) != os.Geteuid() || st.Nlink != 1 || st.Mode&0022 != 0 || st.Size > 64<<10 {
		return nil, st, ErrForeignUnit
	}
	b, err := io.ReadAll(io.LimitReader(f, 64<<10+1))
	if len(b) > 64<<10 {
		return nil, st, ErrForeignUnit
	}
	return b, st, err
}
func recoveryInactive(ctx context.Context, r Runner) error {
	if r == nil {
		return errors.New("service manager unavailable")
	}
	for _, property := range []string{"ActiveState", "MainPID", "ActiveState"} {
		b, err := r.Output(ctx, "show", UnitName, "--property", property, "--value")
		if err != nil {
			return err
		}
		value := strings.TrimSpace(string(b))
		if property == "MainPID" {
			if value != "0" {
				return errors.New("recovery requires MainPID=0")
			}
		} else if value != "inactive" && value != "failed" {
			return errors.New("recovery requires inactive service; foreign processes are never stopped")
		}
	}
	return ctx.Err()
}
func PreviewRecovery(ctx context.Context, r Runner, p Plan) (RecoveryPlan, error) {
	if _, _, err := RecoveryExecutable(p.Executable); err != nil {
		return RecoveryPlan{}, err
	}
	if err := recoveryNoOverrides(ctx, r); err != nil {
		return RecoveryPlan{}, err
	}
	current, err := Preview(p.Executable, p.ConfigPath)
	if err != nil {
		return RecoveryPlan{}, err
	}
	if !samePlan(current, p) {
		return RecoveryPlan{}, ErrForeignUnit
	}
	if err = recoveryInactive(ctx, r); err != nil {
		return RecoveryPlan{}, err
	}
	dir, err := recoveryDirectory(filepath.Dir(p.UnitPath))
	if err != nil {
		return RecoveryPlan{}, err
	}
	defer dir.Close()
	b, st, err := recoverySnapshot(dir, UnitName)
	if err != nil {
		return RecoveryPlan{}, err
	}
	if bytes.Equal(b, p.unit) || bytes.Equal(b, p.legacyUnit) {
		return RecoveryPlan{}, errors.New("recognized unit does not need foreign-unit recovery")
	}
	sum := sha256.Sum256(b)
	digest := hex.EncodeToString(sum[:])
	result := RecoveryPlan{Desired: p, ForeignDigest: digest, Device: uint64(st.Dev), Inode: st.Ino, foreign: b}
	result.EvidencePath = filepath.Join(filepath.Dir(p.UnitPath), fmt.Sprintf(".aegis-recovery-%s-%d.unit", digest, st.Ino))
	binding := sha256.Sum256([]byte(fmt.Sprintf("%s\n%s\n%d\n%d\n%s\n%s", p.UnitPath, p.UnitDigest, result.Device, result.Inode, digest, result.EvidencePath)))
	result.Confirmation = "RECOVER AEGIS " + hex.EncodeToString(binding[:])
	return result, nil
}
func recoveryWrite(dir *os.File, name string, data []byte) error {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err == nil {
		err = dir.Sync()
	}
	return err
}

// ApplyRecovery preserves evidence before atomic exchange. It reloads only;
// readiness and authenticated upgrade remain separate operations.
func ApplyRecovery(ctx context.Context, r Runner, p RecoveryPlan, confirmation string) error {
	if confirmation != p.Confirmation || confirmation == "" {
		return errors.New("exact recovery confirmation required")
	}
	current, err := PreviewRecovery(ctx, r, p.Desired)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, p) {
		return errors.New("recovery preview changed")
	}
	dir, err := recoveryDirectory(filepath.Dir(p.Desired.UnitPath))
	if err != nil {
		return err
	}
	defer dir.Close()
	evidence := filepath.Base(p.EvidencePath)
	if err = recoveryWrite(dir, evidence, p.foreign); err != nil {
		return fmt.Errorf("preserve recovery evidence: %w", err)
	}
	temp := evidence + ".replacement"
	if err = recoveryWrite(dir, temp, p.Desired.unit); err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = unix.Unlinkat(int(dir.Fd()), temp, 0)
		}
	}()
	fresh, err := PreviewRecovery(ctx, r, p.Desired)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(fresh, p) {
		return errors.New("recovery drifted before exchange")
	}
	if err = unix.Renameat2(int(dir.Fd()), temp, int(dir.Fd()), UnitName, unix.RENAME_EXCHANGE); err != nil {
		return err
	}
	displaced, st, err := recoverySnapshot(dir, temp)
	if err != nil || uint64(st.Dev) != p.Device || st.Ino != p.Inode || !bytes.Equal(displaced, p.foreign) {
		// Preserve the exchanged object if a racing writer changed the target.
		rollback := unix.Renameat2(int(dir.Fd()), temp, int(dir.Fd()), UnitName, unix.RENAME_EXCHANGE)
		cleanup = rollback == nil
		return errors.Join(errors.New("recovery exchange identity mismatch"), err, rollback)
	}
	if err = dir.Sync(); err != nil {
		return err
	}
	if err = recoveryInactive(ctx, r); err != nil {
		return err
	}
	// Verify the pathname still resolves to our pinned directory before reload.
	check, err := recoveryDirectory(filepath.Dir(p.Desired.UnitPath))
	if err != nil {
		return err
	}
	defer check.Close()
	a, _ := dir.Stat()
	b, _ := check.Stat()
	if a == nil || b == nil || !os.SameFile(a, b) {
		return errors.New("unit directory changed")
	}
	installed, _, err := recoverySnapshot(dir, UnitName)
	if err != nil || !bytes.Equal(installed, p.Desired.unit) {
		return errors.Join(ErrForeignUnit, err)
	}
	if err = r.Run(ctx, "daemon-reload"); err != nil {
		return err
	}
	if err = recoveryNoOverrides(ctx, r); err != nil {
		return err
	}
	if err = validateLoadedIdentity(ctx, r, p.Desired); err != nil {
		return err
	}
	return recoveryInactive(ctx, r)
}
