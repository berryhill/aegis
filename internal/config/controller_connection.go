//go:build linux

package config

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

var ErrConnectionConfig = errors.New("controller_connection_config_conflict")

func connectionParent(path string) (int, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return -1, ErrConnectionConfig
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, ErrConnectionConfig
	}
	privateSearch := false
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(path), "/"), "/") {
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if e != nil {
			return -1, ErrConnectionConfig
		}
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || (st.Uid != 0 && st.Uid != uint32(os.Geteuid())) || (!privateSearch && st.Mode&0022 != 0 && !(st.Uid == 0 && st.Mode&unix.S_ISVTX != 0)) {
			unix.Close(fd)
			return -1, ErrConnectionConfig
		}
		// An owned mode-0700 search ancestor prevents other users reaching
		// descendants. Do not chmod legitimate checkout traversal context.
		// Access ACLs are denied rather than inferred from mode bits.
		if _, e := unix.Fgetxattr(fd, "system.posix_acl_access", nil); e == nil || (e != unix.ENODATA && e != unix.ENOTSUP) {
			unix.Close(fd)
			return -1, ErrConnectionConfig
		}
		if st.Uid == uint32(os.Geteuid()) && st.Mode&0077 == 0 {
			privateSearch = true
		}
	}
	return fd, nil
}

func connectionReadAt(fd int, name string) ([]byte, error) {
	raw, err := unix.Openat(fd, name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(raw), "controller-connection-metadata")
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return nil, ErrConnectionConfig
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || sys.Uid != uint32(os.Geteuid()) || sys.Nlink != 1 || st.Size() > 1<<20 {
		return nil, ErrConnectionConfig
	}
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || int64(len(b)) != st.Size() {
		return nil, ErrConnectionConfig
	}
	return b, nil
}

// ConnectionSnapshot reads reviewed bytes, not a filesystem compare-and-swap.
func ConnectionSnapshot(path string) ([]byte, error) {
	fd, err := connectionParent(path)
	if err != nil {
		return nil, ErrConnectionConfig
	}
	defer unix.Close(fd)
	b, err := connectionReadAt(fd, filepath.Base(path))
	if err != nil {
		return nil, ErrConnectionConfig
	}
	return b, nil
}

// The sidecar is controller operational metadata, not an Agent grant or secret.
// Never rewrite the operator-owned configuration: no content check followed by
// rename can implement CAS against an uncooperative same-UID writer.
const connectionSuffix = ".controller-codex.json"

type controllerConnectionRecord struct {
	Schema       string               `json:"schema"`
	ConfigSHA256 string               `json:"config_sha256"`
	Binding      ControllerCodexLogin `json:"binding"`
}

func connectionDigest(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

// ReadControllerCodexBinding revalidates the private sidecar and exact owning
// config on every use. Legacy explicit YAML bindings remain supported, but any
// sidecar conflict/drift denies rather than falling back to an old account.
func ReadControllerCodexBinding(path string) (*ControllerCodexLogin, error) {
	before, err := ConnectionSnapshot(path)
	if err != nil {
		return nil, ErrConnectionConfig
	}
	cfg, err := Load(path, nil)
	if err != nil {
		return nil, ErrConnectionConfig
	}
	fd, err := connectionParent(path)
	if err != nil {
		return nil, ErrConnectionConfig
	}
	defer unix.Close(fd)
	data, err := connectionReadAt(fd, filepath.Base(path)+connectionSuffix)
	var binding *ControllerCodexLogin
	if errors.Is(err, syscall.ENOENT) {
		binding = cfg.Credentials.ControllerCodexLogin
	} else {
		var parent unix.Stat_t
		if err != nil || cfg.Credentials.ControllerCodexLogin != nil || unix.Fstat(fd, &parent) != nil || parent.Uid != uint32(os.Geteuid()) || parent.Mode&0077 != 0 {
			return nil, ErrConnectionConfig
		}
		var record controllerConnectionRecord
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if dec.Decode(&record) != nil || dec.Decode(new(any)) != io.EOF || record.Schema != "controller-codex-connection.v1" || record.ConfigSHA256 != connectionDigest(before) {
			return nil, ErrConnectionConfig
		}
		binding = &record.Binding
		cfg.Credentials.ControllerCodexLogin = binding
		if cfg.Validate() != nil {
			return nil, ErrConnectionConfig
		}
	}
	after, err := ConnectionSnapshot(path)
	if err != nil || !bytes.Equal(before, after) {
		return nil, ErrConnectionConfig
	}
	return binding, nil
}

// Publish a fully synced inode without replacing any existing record. This is
// atomic no-replace publication, NOT CAS on the independent owning config.
func publishControllerConnection(fd int, staged, target string) error {
	if err := unix.Linkat(fd, staged, fd, target, 0); err != nil {
		return err
	}
	if err := unix.Unlinkat(fd, staged, 0); err != nil {
		return err
	}
	return unix.Fsync(fd)
}

func ConnectControllerCodex(path string, expected []byte, binding ControllerCodexLogin) error {
	return connectControllerCodex(path, expected, binding, publishControllerConnection)
}

func connectControllerCodex(path string, expected []byte, binding ControllerCodexLogin, publish func(int, string, string) error) error {
	current, err := ConnectionSnapshot(path)
	if err != nil || !bytes.Equal(current, expected) {
		return ErrConnectionConfig
	}
	cfg, err := Load(path, nil)
	if err != nil || cfg.Credentials.ControllerCodexLogin != nil {
		return ErrConnectionConfig
	}
	cfg.Credentials.ControllerCodexLogin = &binding
	if cfg.Validate() != nil {
		return ErrConnectionConfig
	}
	fd, err := connectionParent(path)
	if err != nil {
		return ErrConnectionConfig
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return ErrConnectionConfig
	}
	var entropy [16]byte
	if _, err = rand.Read(entropy[:]); err != nil {
		return ErrConnectionConfig
	}
	staged := ".controller-codex-" + hex.EncodeToString(entropy[:]) + ".yaml"
	raw, err := unix.Openat(fd, staged, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return ErrConnectionConfig
	}
	defer unix.Unlinkat(fd, staged, 0)
	f := os.NewFile(uintptr(raw), staged)
	data, err := json.Marshal(controllerConnectionRecord{"controller-codex-connection.v1", connectionDigest(expected), binding})
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return ErrConnectionConfig
	}
	current, err = ConnectionSnapshot(path)
	if err != nil || !bytes.Equal(current, expected) {
		return ErrConnectionConfig
	}
	if publish(fd, staged, filepath.Base(path)+connectionSuffix) != nil {
		return ErrConnectionConfig
	}
	// A late config edit is never overwritten or rolled back. Leave the inert
	// record for typed show/reconciliation, and deny if its binding no longer fits.
	actual, err := ReadControllerCodexBinding(path)
	if err != nil || actual == nil || *actual != binding {
		return ErrConnectionConfig
	}
	return nil
}
