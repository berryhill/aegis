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
	Schema                string               `json:"schema"`
	ConfigSHA256          string               `json:"config_sha256"`
	PreviousSHA256        string               `json:"previous_sha256"`
	Generation            int                  `json:"generation"`
	Binding               ControllerCodexLogin `json:"binding"`
	PreviousConfigSHA256  string               `json:"previous_config_sha256,omitempty"`
	PreviousHistorySHA256 string               `json:"previous_history_sha256,omitempty"`
}

// Retain every connection fact. Contiguous no-replace publication is the
// single-winner update boundary; a gap, unknown record or config drift denies.
func readControllerReplacements(fd int, base string, snapshot []byte, current *ControllerCodexLogin) (*ControllerCodexLogin, int, error) {
	state := ControllerCodexConfigReview{ConfigSHA256: connectionDigest(snapshot)}
	if raw, err := connectionReadAt(fd, base+connectionSuffix); err == nil {
		var first controllerConnectionRecord
		if json.Unmarshal(raw, &first) != nil {
			return nil, 0, ErrConnectionConfig
		}
		state.ConfigSHA256 = first.ConfigSHA256
		state.HistorySHA256 = connectionDigest(raw)
	}
	return readControllerHistory(fd, base, snapshot, current, &state)
}

func readControllerHistory(fd int, base string, snapshot []byte, current *ControllerCodexLogin, state *ControllerCodexConfigReview) (*ControllerCodexLogin, int, error) {
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
	records := make([]controllerReplacementRecord, count)
	encoded := make([][]byte, count)
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
		records[i-1], encoded[i-1] = record, raw
	}
	// Replay each transition against its own exact configuration, reconstructed
	// from current bytes. Never persist private configuration backups or rewrite
	// historical facts. A pending addition remains denied at runtime/readback.
	configs := make([][]byte, count)
	if count > 0 {
		latest := records[count-1].ConfigSHA256
		if connectionDigest(snapshot) != latest {
			var ok bool
			snapshot, ok = nativeTrustPredecessor(snapshot, latest)
			if !ok {
				return nil, 0, ErrConnectionConfig
			}
		}
		for i := count - 1; i >= 0; i-- {
			record := records[i]
			if connectionDigest(snapshot) != record.ConfigSHA256 {
				return nil, 0, ErrConnectionConfig
			}
			configs[i] = snapshot
			if record.Schema == "controller-codex-replacement.v2" {
				var ok bool
				snapshot, ok = nativeTrustPredecessor(snapshot, record.PreviousConfigSHA256)
				if !ok {
					return nil, 0, ErrConnectionConfig
				}
			}
		}
		if connectionDigest(snapshot) != state.ConfigSHA256 {
			return nil, 0, ErrConnectionConfig
		}
	}
	for index, record := range records {
		i, raw := index+1, encoded[index]
		prior, _ := json.Marshal(current)
		if record.PreviousSHA256 != connectionDigest(prior) || record.Generation != i {
			return nil, 0, ErrConnectionConfig
		}
		switch record.Schema {
		case "controller-codex-replacement.v1":
			if record.ConfigSHA256 != state.ConfigSHA256 || record.PreviousConfigSHA256 != "" || record.PreviousHistorySHA256 != "" {
				return nil, 0, ErrConnectionConfig
			}
		case "controller-codex-replacement.v2":
			if current == nil || record.Binding != *current || record.PreviousConfigSHA256 != state.ConfigSHA256 || record.PreviousHistorySHA256 != state.HistorySHA256 || !nativeTrustAddition(configs[index], state.ConfigSHA256) {
				return nil, 0, ErrConnectionConfig
			}
		default:
			return nil, 0, ErrConnectionConfig
		}
		state.ConfigSHA256 = record.ConfigSHA256
		state.HistorySHA256 = connectionDigest([]byte(state.HistorySHA256 + connectionDigest(raw)))
		b := record.Binding
		current = &b
	}
	return current, count, nil
}

// Replace only the exact reviewed existing operational binding. This never
// edits operator YAML, auth files, credentials, Agent charters, or prior facts.
func ReplaceControllerCodex(path string, expected []byte, previous, binding ControllerCodexLogin) error {
	return replaceControllerCodex(path, expected, previous, binding, nil)
}

// Called only by the explicit one-use app review path. Runtime/show never call it.
func ReplaceControllerCodexReviewed(path string, expected []byte, previous, binding ControllerCodexLogin, review ControllerCodexConfigReview) error {
	if !review.Additive || previous != binding {
		return ErrConnectionConfig
	}
	return replaceControllerCodex(path, expected, previous, binding, &review)
}

func replacementPredecessor(path string, review *ControllerCodexConfigReview) (*ControllerCodexLogin, error) {
	if review == nil {
		return ReadControllerCodexBinding(path)
	}
	b, state, err := ReadControllerCodexForReview(path)
	if err != nil || state != *review {
		return nil, ErrConnectionConfig
	}
	return b, nil
}

func replaceControllerCodex(path string, expected []byte, previous, binding ControllerCodexLogin, review *ControllerCodexConfigReview) error {
	current, err := ConnectionSnapshot(path)
	if err != nil || !bytes.Equal(current, expected) {
		return ErrConnectionConfig
	}
	cfg, err := Load(path, nil)
	if err != nil {
		return ErrConnectionConfig
	}
	actual, err := replacementPredecessor(path, review)
	if err != nil || actual == nil || *actual != previous || previous == binding && review == nil {
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
	record := controllerReplacementRecord{Schema: "controller-codex-replacement.v1", ConfigSHA256: connectionDigest(expected), PreviousSHA256: connectionDigest(prior), Generation: count + 1, Binding: binding}
	if review != nil {
		record.Schema = "controller-codex-replacement.v2"
		record.PreviousConfigSHA256 = review.ConfigSHA256
		record.PreviousHistorySHA256 = review.HistorySHA256
	}
	data, err := json.Marshal(record)
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
	actual, err = replacementPredecessor(path, review)
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
