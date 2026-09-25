package manager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
)

// CertificationCheckpoint retains only ordered passed-case metadata. Its checksum
// detects accidental corruption; it does not authenticate against a same-UID writer.
type CertificationCheckpoint struct {
	SchemaVersion string        `json:"schema_version"`
	PrincipalID   string        `json:"principal_id"`
	Certification Certification `json:"certification"`
	CreatedAt     time.Time     `json:"created_at"`
	ExpiresAt     time.Time     `json:"expires_at"`
}

type checkpointEnvelope struct {
	Checkpoint CertificationCheckpoint `json:"checkpoint"`
	Checksum   string                  `json:"checksum"`
}

const checkpointSchemaVersion = "aegis.manager.certification-checkpoint.v1"

var ErrCertificationCheckpointExpired = errors.New("certification checkpoint expired")

func checkpointTuple(c Certification) Certification {
	c.Results = nil
	c.CertifiedAt = time.Time{}
	return c
}

func validateCheckpointPrefix(results []ConformanceResult) error {
	corpus := ConformanceCorpus()
	if len(results) > len(corpus) {
		return errors.New("checkpoint exceeds conformance corpus")
	}
	for i, result := range results {
		if result.CaseID != corpus[i].ID || !result.Passed || result.Reason != "passed" {
			return errors.New("checkpoint is not an ordered passed prefix")
		}
	}
	return nil
}

func validateCheckpoint(cp CertificationCheckpoint) error {
	c := cp.Certification
	if cp.SchemaVersion != checkpointSchemaVersion || strings.TrimSpace(cp.PrincipalID) == "" || cp.PrincipalID != strings.TrimSpace(cp.PrincipalID) || cp.CreatedAt.IsZero() || cp.ExpiresAt.IsZero() || !cp.ExpiresAt.After(cp.CreatedAt) || !c.CertifiedAt.IsZero() {
		return errors.New("invalid checkpoint principal, version, or lifetime")
	}
	if c.SchemaVersion != "aegis.manager.certification.v1" || c.CandidateID == "" || c.ArtifactName == "" || len(c.ArtifactDigest) != 71 || !strings.HasPrefix(c.ArtifactDigest, "sha256:") || c.ContextLength < 64000 || c.HermesVersion == "" || c.OllamaVersion == "" || c.InstructionDigest != digestString(ManagerSystemInstruction()) || c.ResponseSchema != ResponseSchemaVersion || c.CorpusDigest != CorpusDigest() {
		return errors.New("checkpoint certification identity is incomplete or stale")
	}
	if _, err := hex.DecodeString(c.ArtifactDigest[7:]); err != nil {
		return fmt.Errorf("invalid artifact digest: %w", err)
	}
	known := false
	for _, candidate := range Candidates() {
		if c.CandidateID == candidate.ID && c.ArtifactName == candidate.OllamaName {
			known = true
			break
		}
	}
	if !known {
		return errors.New("unknown candidate/model pair")
	}
	return validateCheckpointPrefix(c.Results)
}

func checkpointDirectory(path string, create bool) error {
	dir := filepath.Dir(path)
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	current := string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(absolute, current), current) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && create {
			if err = os.Mkdir(current, 0700); err == nil || errors.Is(err, os.ErrExist) {
				info, err = os.Lstat(current)
			}
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("checkpoint directory has unsafe component")
		}
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0700 {
		return errors.New("checkpoint directory must be owner-only")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Getuid()) {
		return errors.New("checkpoint directory owner mismatch")
	}
	return nil
}

// openCheckpointFile denies symlinks and hardlinks, checking the opened inode
// against the pathname to avoid an lstat/open substitution.
func openCheckpointFile(path string, flags int, mode uint32) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, mode)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err == nil {
		var named os.FileInfo
		named, err = os.Lstat(path)
		if err == nil {
			a, aok := info.Sys().(*syscall.Stat_t)
			b, bok := named.Sys().(*syscall.Stat_t)
			if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !aok || !bok || a.Nlink != 1 || a.Uid != uint32(os.Getuid()) || a.Ino != b.Ino || a.Dev != b.Dev || named.Mode()&os.ModeSymlink != 0 {
				err = errors.New("unsafe checkpoint file")
			}
		}
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func readCheckpoint(path string) (CertificationCheckpoint, bool, error) {
	file, err := openCheckpointFile(path, syscall.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return CertificationCheckpoint{}, false, nil
	}
	if err != nil {
		return CertificationCheckpoint{}, true, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return CertificationCheckpoint{}, true, err
	}
	if len(data) > 1<<20 || validateJSONObject(data, 32) != nil {
		return CertificationCheckpoint{}, true, errors.New("malformed checkpoint")
	}
	var envelope checkpointEnvelope
	if err := strictDecode(data, &envelope); err != nil {
		return CertificationCheckpoint{}, true, err
	}
	encoded, err := json.Marshal(envelope.Checkpoint)
	if err != nil {
		return CertificationCheckpoint{}, true, err
	}
	sum := sha256.Sum256(encoded)
	if envelope.Checksum != "sha256:"+hex.EncodeToString(sum[:]) {
		return CertificationCheckpoint{}, true, errors.New("checkpoint checksum mismatch")
	}
	if err := validateCheckpoint(envelope.Checkpoint); err != nil {
		return CertificationCheckpoint{}, true, err
	}
	return envelope.Checkpoint, true, nil
}

// LoadCertificationCheckpoint reports false only for a missing checkpoint.
func LoadCertificationCheckpoint(path string, expected Certification, principalID string, now time.Time) (CertificationCheckpoint, bool, error) {
	if path == "" || filepath.Base(path) == "." {
		return CertificationCheckpoint{}, true, errors.New("checkpoint path required")
	}
	if err := checkpointDirectory(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CertificationCheckpoint{}, false, nil
		}
		return CertificationCheckpoint{}, true, err
	}
	cp, present, err := readCheckpoint(path)
	if err != nil || !present {
		return cp, present, err
	}
	if err := validateCheckpoint(cp); err != nil {
		return CertificationCheckpoint{}, true, err
	}
	if principalID == "" || cp.PrincipalID != principalID || !reflect.DeepEqual(checkpointTuple(cp.Certification), checkpointTuple(expected)) || now.Before(cp.CreatedAt) {
		return CertificationCheckpoint{}, true, errors.New("checkpoint identity or lifetime mismatch")
	}
	if !now.Before(cp.ExpiresAt) {
		return CertificationCheckpoint{}, true, ErrCertificationCheckpointExpired
	}
	return cp, true, nil
}

// RetireExpiredCertificationCheckpoint is an explicit, non-authorizing reset
// for an expired exact campaign. The caller must authenticate and audit its
// intent separately. Malformed or mismatched records are never retired here.
func RetireExpiredCertificationCheckpoint(path string, expected Certification, principalID string, now time.Time) error {
	if err := checkpointDirectory(path, false); err != nil {
		return err
	}
	lock, err := openCheckpointFile(path+".lock", syscall.O_RDWR|syscall.O_CREAT, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	lockedInfo, statErr := lock.Stat()
	pathInfo, pathErr := os.Lstat(path + ".lock")
	if statErr != nil || pathErr != nil || !os.SameFile(lockedInfo, pathInfo) {
		return errors.New("checkpoint lock changed while waiting")
	}
	cp, present, err := readCheckpoint(path)
	if err != nil || !present {
		return errors.New("checkpoint cannot be safely retired")
	}
	if cp.PrincipalID != principalID || !reflect.DeepEqual(checkpointTuple(cp.Certification), checkpointTuple(expected)) || now.Before(cp.ExpiresAt) || now.Before(cp.CreatedAt) {
		return errors.New("checkpoint is not the exact expired campaign")
	}
	// A create-only retired name preserves the prior evidence for review.
	retired := path + ".expired." + fmt.Sprint(now.UnixNano())
	if _, err := os.Lstat(retired); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errors.New("checkpoint retired destination is occupied or unsafe")
	}
	if err := os.Rename(path, retired); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// SaveCertificationCheckpoint atomically advances the exact passed prefix under
// an advisory file lock; expectedPrevious is -1 only when the file is absent.
func SaveCertificationCheckpoint(path string, checkpoint CertificationCheckpoint, expectedPrevious int) error {
	if path == "" || filepath.Base(path) == "." {
		return errors.New("checkpoint path required")
	}
	if expectedPrevious < -1 {
		return errors.New("invalid expected predecessor")
	}
	if err := validateCheckpoint(checkpoint); err != nil {
		return err
	}
	if err := checkpointDirectory(path, true); err != nil {
		return err
	}
	lock, err := openCheckpointFile(path+".lock", syscall.O_RDWR|syscall.O_CREAT, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	lockedInfo, statErr := lock.Stat()
	pathInfo, pathErr := os.Lstat(path + ".lock")
	if statErr != nil || pathErr != nil || !os.SameFile(lockedInfo, pathInfo) {
		return errors.New("checkpoint lock changed while waiting")
	}
	previous, present, err := readCheckpoint(path)
	if err != nil {
		return err
	}
	if !present && expectedPrevious != -1 || present && (expectedPrevious < 0 || len(previous.Certification.Results) != expectedPrevious) {
		return errors.New("checkpoint predecessor conflict")
	}
	if present {
		if previous.PrincipalID != checkpoint.PrincipalID || previous.SchemaVersion != checkpoint.SchemaVersion || !previous.CreatedAt.Equal(checkpoint.CreatedAt) || !previous.ExpiresAt.Equal(checkpoint.ExpiresAt) || !reflect.DeepEqual(checkpointTuple(previous.Certification), checkpointTuple(checkpoint.Certification)) {
			return errors.New("checkpoint predecessor identity mismatch")
		}
		if len(checkpoint.Certification.Results) <= expectedPrevious || (expectedPrevious > 0 && !reflect.DeepEqual(previous.Certification.Results, checkpoint.Certification.Results[:expectedPrevious])) {
			return errors.New("checkpoint does not advance predecessor")
		}
	}
	encoded, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(encoded)
	payload, err := json.Marshal(checkpointEnvelope{Checkpoint: checkpoint, Checksum: "sha256:" + hex.EncodeToString(sum[:])})
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(0600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	// Recheck the destination while holding the lock; never replace an unsafe link.
	if existing, err := openCheckpointFile(path, syscall.O_RDONLY, 0); err == nil {
		_ = existing.Close()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
