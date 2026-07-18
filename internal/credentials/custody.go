package credentials

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/crypto/chacha20poly1305"
)

type FileCustodian struct {
	metadata KEKMetadata
	key      []byte
}

type keyFile struct {
	FormatVersion uint16 `json:"format_version"`
	ID            string `json:"id"`
	Version       uint64 `json:"version"`
	Key           string `json:"key"`
}

// CreateHostKey creates the explicitly weaker host-file custody fallback. The
// file must be kept outside authority database backups and replaced by a
// systemd encrypted service credential for production deployments.
func CreateHostKey(path, id string) error {
	if !ValidateIdentifier(id) {
		return errors.New("invalid key-encryption key identifier")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	key := make([]byte, chacha20poly1305.KeySize)
	defer wipe(key)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	value, err := json.Marshal(keyFile{FormatVersion: FormatVersion, ID: id, Version: 1, Key: base64.StdEncoding.EncodeToString(key)})
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(append(value, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func LoadFileCustodian(path string) (*FileCustodian, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	permissions := info.Mode().Perm()
	if !info.Mode().IsRegular() || permissions&0077 != 0 || permissions&0100 != 0 || permissions&0400 == 0 || !ok || int(stat.Uid) != os.Geteuid() || int(stat.Gid) != os.Getegid() || stat.Nlink != 1 {
		return nil, errors.New("key-encryption key file must be a regular file with no group or other permissions")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var stored keyFile
	if err = decoder.Decode(&stored); err != nil {
		return nil, errors.New("decode key-encryption key credential")
	}
	if decoder.Decode(&struct{}{}) != io.EOF || stored.FormatVersion != FormatVersion || !ValidateIdentifier(stored.ID) || stored.Version == 0 {
		return nil, errors.New("key-encryption key credential is invalid")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(stored.Key)
	if err != nil || len(key) != chacha20poly1305.KeySize {
		wipe(key)
		return nil, errors.New("key-encryption key credential is invalid")
	}
	return &FileCustodian{metadata: KEKMetadata{ID: stored.ID, Version: stored.Version}, key: key}, nil
}

func (c *FileCustodian) ActiveKEK(ctx context.Context, fn func(KEKMetadata, []byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	copyKey := append([]byte(nil), c.key...)
	defer wipe(copyKey)
	return fn(c.metadata, copyKey)
}

func (c *FileCustodian) KEK(ctx context.Context, id string, version uint64, fn func([]byte) error) error {
	if id != c.metadata.ID || version != c.metadata.Version {
		return errors.New("requested key-encryption key version is unavailable")
	}
	return c.ActiveKEK(ctx, func(_ KEKMetadata, key []byte) error { return fn(key) })
}

func (c *FileCustodian) Close() { wipe(c.key); c.key = nil }
