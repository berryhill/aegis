package principalauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/crypto/scrypt"
)

const (
	FileName        = "principal-password.json"
	AlgorithmScrypt = "scrypt"
	MinimumN        = 1 << 15
	schemaVersion   = "aegis.dev/principal-password/v1"
	saltBytes       = 32
	digestBytes     = 32
)

var (
	ErrAuthentication  = errors.New("principal authentication failed")
	ErrUnsafeArtifact  = errors.New("principal authentication artifact is unsafe")
	ErrVerifierChanged = errors.New("principal authentication verifier changed")
)

type Record struct {
	SchemaVersion string `json:"schema_version"`
	PrincipalID   string `json:"principal_id"`
	Algorithm     string `json:"algorithm"`
	N             int    `json:"n"`
	R             int    `json:"r"`
	P             int    `json:"p"`
	Salt          string `json:"salt"`
	Digest        string `json:"digest"`
}

func Enroll(principalID string, password []byte) (Record, error) {
	if principalID == "" || len(password) < 12 || len(password) > 1024 {
		return Record{}, errors.New("principal password must be between 12 and 1024 bytes")
	}
	for _, value := range password {
		if value == 0 || value == '\r' || value == '\n' {
			return Record{}, errors.New("principal password contains unsupported control bytes")
		}
	}
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return Record{}, fmt.Errorf("generate principal password salt: %w", err)
	}
	digest, err := scrypt.Key(password, salt, MinimumN, 8, 1, digestBytes)
	if err != nil {
		return Record{}, fmt.Errorf("derive principal password verifier: %w", err)
	}
	return Record{SchemaVersion: schemaVersion, PrincipalID: principalID, Algorithm: AlgorithmScrypt, N: MinimumN, R: 8, P: 1, Salt: base64.RawStdEncoding.EncodeToString(salt), Digest: base64.RawStdEncoding.EncodeToString(digest)}, nil
}

func (r Record) validate() ([]byte, []byte, error) {
	if r.SchemaVersion != schemaVersion || r.PrincipalID == "" || r.Algorithm != AlgorithmScrypt || r.N != MinimumN || r.R != 8 || r.P != 1 {
		return nil, nil, errors.New("unsupported principal password verifier parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(r.Salt)
	if err != nil || len(salt) != saltBytes {
		return nil, nil, errors.New("invalid principal password verifier salt")
	}
	digest, err := base64.RawStdEncoding.DecodeString(r.Digest)
	if err != nil || len(digest) != digestBytes {
		return nil, nil, errors.New("invalid principal password verifier digest")
	}
	return salt, digest, nil
}

func (r Record) Verify(password []byte) error {
	salt, expected, err := r.validate()
	if err != nil {
		return ErrAuthentication
	}
	observed, err := scrypt.Key(password, salt, r.N, r.R, r.P, len(expected))
	if err != nil || subtle.ConstantTimeCompare(observed, expected) != 1 {
		return ErrAuthentication
	}
	return nil
}

func (r Record) Marshal() ([]byte, error) {
	if _, _, err := r.validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func Load(path string) (Record, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Record{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ok || int(stat.Uid) != os.Geteuid() || stat.Nlink != 1 {
		return Record{}, ErrUnsafeArtifact
	}
	file, err := os.Open(path)
	if err != nil {
		return Record{}, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return Record{}, err
	}
	openedStat, openedOK := openedInfo.Sys().(*syscall.Stat_t)
	if !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() || openedInfo.Mode().Perm() != 0600 || !openedOK || int(openedStat.Uid) != os.Geteuid() || openedStat.Nlink != 1 {
		return Record{}, ErrUnsafeArtifact
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var record Record
	if err = decoder.Decode(&record); err != nil {
		return Record{}, fmt.Errorf("decode principal password verifier: %w", err)
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Record{}, errors.New("principal password verifier has trailing data")
	}
	if _, _, err = record.validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func Publish(path string, record Record) error {
	encoded, err := record.Marshal()
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err = os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	if err = os.Chmod(parent, 0700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".principal-password-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(encoded)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Link(temporaryPath, path); err != nil {
		return fmt.Errorf("publish principal password verifier: %w", err)
	}
	_ = os.Remove(temporaryPath)
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	err = directory.Sync()
	return errors.Join(err, directory.Close())
}

// Replace publishes replacement only when path still contains expected. The
// owner-only lock serializes competing Aegis replacements, and rename keeps
// readers on either the complete old record or the complete new record.
func Replace(path string, expected, replacement Record) error {
	return replace(path, expected, replacement, nil)
}

func replace(path string, expected, replacement Record, beforeActivate func() error) error {
	if _, err := replacement.Marshal(); err != nil {
		return err
	}
	parent := filepath.Dir(path)
	lock, err := os.OpenFile(filepath.Join(parent, ".principal-password.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = lock.Chmod(0600); err != nil {
		return err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	current, err := Load(path)
	if err != nil {
		return err
	}
	if current != expected {
		return ErrVerifierChanged
	}
	encoded, err := replacement.Marshal()
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".principal-password-replacement-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(encoded)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if beforeActivate != nil {
		if err = beforeActivate(); err != nil {
			return err
		}
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace principal password verifier: %w", err)
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	err = directory.Sync()
	return errors.Join(err, directory.Close())
}
