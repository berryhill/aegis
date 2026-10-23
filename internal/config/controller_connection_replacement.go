//go:build linux

package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const maxControllerConnectionReplacements = 64

type controllerReplacementRecord struct {
	Schema         string               `json:"schema"`
	ConfigSHA256   string               `json:"config_sha256"`
	PreviousSHA256 string               `json:"previous_sha256"`
	Generation     int                  `json:"generation"`
	Binding        ControllerCodexLogin `json:"binding"`
}

// Retain every connection fact. Contiguous no-replace publication is the
// single-winner update boundary; a gap, unknown record or config drift denies.
func readControllerReplacements(fd int, base string, snapshot []byte, current *ControllerCodexLogin) (*ControllerCodexLogin, int, error) {
	prefix := base + connectionSuffix + ".replacement-"
	scan, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, 0, ErrConnectionConfig
	}
	f := os.NewFile(uintptr(scan), "controller-connection-history")
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil, 0, ErrConnectionConfig
	}
	count := 0
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			count++
		}
	}
	if count > maxControllerConnectionReplacements || count > 0 && current == nil {
		return nil, 0, ErrConnectionConfig
	}
	for i := 1; i <= count; i++ {
		raw, err := connectionReadAt(fd, fmt.Sprintf("%s%06d", prefix, i))
		if err != nil {
			return nil, 0, ErrConnectionConfig
		}
		var record controllerReplacementRecord
		if json.Unmarshal(raw, &record) != nil {
			return nil, 0, ErrConnectionConfig
		}
		canonical, err := json.Marshal(record)
		if err != nil || !bytes.Equal(raw, canonical) {
			return nil, 0, ErrConnectionConfig
		}
		prior, _ := json.Marshal(current)
		if record.Schema != "controller-codex-replacement.v1" || record.ConfigSHA256 != connectionDigest(snapshot) || record.PreviousSHA256 != connectionDigest(prior) || record.Generation != i {
			return nil, 0, ErrConnectionConfig
		}
		b := record.Binding
		current = &b
	}
	return current, count, nil
}

// Replace only the exact reviewed existing operational binding. This never
// edits operator YAML, auth files, credentials, Agent charters, or prior facts.
func ReplaceControllerCodex(path string, expected []byte, previous, binding ControllerCodexLogin) error {
	current, err := ConnectionSnapshot(path)
	if err != nil || !bytes.Equal(current, expected) {
		return ErrConnectionConfig
	}
	cfg, err := Load(path, nil)
	if err != nil {
		return ErrConnectionConfig
	}
	actual, err := ReadControllerCodexBinding(path)
	if err != nil || actual == nil || *actual != previous || previous == binding {
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
	// Count only a fully revalidated chain rooted in the immutable base binding.
	baseCfg, err := Load(path, nil)
	if err != nil {
		return ErrConnectionConfig
	}
	baseBinding := baseCfg.Credentials.ControllerCodexLogin
	if raw, e := connectionReadAt(fd, filepath.Base(path)+connectionSuffix); e == nil {
		var first controllerConnectionRecord
		if json.Unmarshal(raw, &first) != nil {
			return ErrConnectionConfig
		}
		baseBinding = &first.Binding
	}
	observed, count, err := readControllerReplacements(fd, filepath.Base(path), expected, baseBinding)
	if err != nil || count >= maxControllerConnectionReplacements || observed == nil || *observed != previous {
		return ErrConnectionConfig
	}
	prior, _ := json.Marshal(&previous)
	data, err := json.Marshal(controllerReplacementRecord{Schema: "controller-codex-replacement.v1", ConfigSHA256: connectionDigest(expected), PreviousSHA256: connectionDigest(prior), Generation: count + 1, Binding: binding})
	if err != nil {
		return ErrConnectionConfig
	}
	var entropy [16]byte
	if _, err = rand.Read(entropy[:]); err != nil {
		return ErrConnectionConfig
	}
	staged := ".controller-codex-replacement-" + hex.EncodeToString(entropy[:])
	raw, err := unix.Openat(fd, staged, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return ErrConnectionConfig
	}
	defer unix.Unlinkat(fd, staged, 0)
	f := os.NewFile(uintptr(raw), staged)
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrConnectionConfig
	}
	current, err = ConnectionSnapshot(path)
	if err != nil || !bytes.Equal(current, expected) {
		return ErrConnectionConfig
	}
	actual, err = ReadControllerCodexBinding(path)
	if err != nil || actual == nil || *actual != previous {
		return ErrConnectionConfig
	}
	target := fmt.Sprintf("%s%s.replacement-%06d", filepath.Base(path), connectionSuffix, count+1)
	if publishControllerConnection(fd, staged, target) != nil {
		return ErrConnectionConfig
	}
	actual, err = ReadControllerCodexBinding(path)
	if err != nil || actual == nil || *actual != binding {
		return ErrConnectionConfig
	}
	return nil
}
