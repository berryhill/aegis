package badger

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	physical "github.com/berryhill/aegis/internal/persistence/recoveryphysical"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const recoveryByteLimit int64 = 16 << 30
const recoveryFileLimit = 4096

// Hash all logical bytes, but do not materialize zero blocks in sparse logs.
func recoveryCopy(ctx context.Context, source, destination string) (string, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || !ownedByCurrentEUID(info) || st.Nlink != 1 || info.Mode().Perm()&0022 != 0 || info.Size() > recoveryByteLimit {
		return "", errors.New("unsafe recovery file")
	}
	fd, err := unix.Open(source, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	in := os.NewFile(uintptr(fd), source)
	defer in.Close()
	opened, err := in.Stat()
	if err != nil || !os.SameFile(info, opened) || opened.Size() != info.Size() {
		return "", errors.New("recovery source changed")
	}
	var out *os.File
	if destination != "" {
		out, err = os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return "", err
		}
		defer out.Close()
	}
	h := sha256.New()
	buf := make([]byte, 1<<20)
	zero := make([]byte, len(buf))
	remaining := info.Size()
	reader := physical.LogicalReader(in)
	for remaining > 0 {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		n := int64(len(buf))
		if remaining < n {
			n = remaining
		}
		if _, err = io.ReadFull(reader, buf[:n]); err != nil {
			return "", err
		}
		h.Write(buf[:n])
		if out != nil {
			if bytes.Equal(buf[:n], zero[:n]) {
				_, err = out.Seek(n, io.SeekCurrent)
			} else {
				_, err = out.Write(buf[:n])
			}
			if err != nil {
				return "", err
			}
		}
		remaining -= n
	}
	var extra [1]byte
	if n, e := reader.Read(extra[:]); n != 0 || e != io.EOF {
		return "", errors.New("recovery source grew")
	}
	after, err := in.Stat()
	if err != nil || after.Size() != info.Size() || after.ModTime() != info.ModTime() || after.Mode() != info.Mode() || !physical.SameChangeTime(info, after) {
		return "", errors.New("recovery source changed")
	}
	if out != nil {
		if err = out.Truncate(info.Size()); err != nil {
			return "", err
		}
		if err = out.Sync(); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func recoveryDirectory(ctx context.Context, source, dest string) (string, error) {
	if err := requireSecureDirectory(source); err != nil {
		return "", err
	}
	if err := requireSecureDirectory(filepath.Dir(source)); err != nil {
		return "", err
	}
	original, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	if dest != "" {
		if err := requireSecureDirectory(filepath.Dir(dest)); err != nil {
			return "", err
		}
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return "", err
	}
	if len(entries) > recoveryFileLimit {
		return "", errors.New("recovery file bound")
	}
	if dest != "" {
		if err = os.Mkdir(dest, 0700); err != nil {
			return "", err
		}
	}
	inventory := map[string]string{}
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return "", err
		}
		total += info.Size()
		if total > recoveryByteLimit {
			return "", errors.New("recovery byte bound")
		}
		target := ""
		if dest != "" {
			target = filepath.Join(dest, e.Name())
		}
		sum, err := recoveryCopy(ctx, filepath.Join(source, e.Name()), target)
		if err != nil {
			return "", err
		}
		inventory[e.Name()] = sum
	}
	current, err := os.Lstat(source)
	if err != nil || !os.SameFile(original, current) {
		return "", errors.New("recovery directory changed")
	}
	after, err := os.ReadDir(source)
	if err != nil || len(after) != len(entries) {
		return "", errors.New("recovery inventory changed")
	}
	for i := range after {
		if after[i].Name() != entries[i].Name() {
			return "", errors.New("recovery inventory changed")
		}
	}
	if dest != "" {
		if err = syncDirectory(dest); err != nil {
			return "", err
		}
	}
	b, _ := json.Marshal(inventory)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
