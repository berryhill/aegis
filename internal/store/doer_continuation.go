package store

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/berryhill/aegis/internal/continuation"
)

// Continuation approvals are canonical operational documents (STORAGE class 3).
// Their purpose-specific signer is controller-only custody, never the audit key,
// credential authority, model configuration, or session-authority database.
func protectedContinuationRead(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 128*1024 {
		return nil, continuation.ErrInvalid
	}
	return io.ReadAll(f)
}

func (s *Store) continuationKey(create bool) (ed25519.PrivateKey, error) {
	dir := filepath.Join(s.root, "controller-continuation")
	if err := s.secureStoreDirectory(dir, create); err != nil {
		return nil, err
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0700 {
		return nil, continuation.ErrInvalid
	}
	path := filepath.Join(dir, "ed25519-seed")
	seed, err := protectedContinuationRead(path)
	if errors.Is(err, os.ErrNotExist) && create {
		seed = make([]byte, ed25519.SeedSize)
		if _, err = rand.Read(seed); err != nil {
			return nil, err
		}
		if err = writeBytesCreateOnly(path, seed); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize {
		return nil, continuation.ErrInvalid
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// DoerContinuationPublicKey never initializes custody during readback.
func (s *Store) DoerContinuationPublicKey() (ed25519.PublicKey, error) {
	var public ed25519.PublicKey
	err := s.withLock(func() error {
		key, e := s.continuationKey(false)
		if e != nil {
			return e
		}
		public = append(ed25519.PublicKey(nil), key.Public().(ed25519.PublicKey)...)
		return nil
	})
	return public, err
}

func (s *Store) continuationPath(id string) (string, error) {
	if len(id) != 73 || id[:9] != "doercont-" {
		return "", continuation.ErrInvalid
	}
	for _, c := range id[9:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", continuation.ErrInvalid
		}
	}
	return filepath.Join(s.root, "controller-continuation", "approvals", id+".json"), nil
}

// ApproveDoerContinuation signs ONLY the validated typed one-intent approval.
// Exact replay is read/verified; changed content in the stable slot conflicts.
func (s *Store) ApproveDoerContinuation(a continuation.Approval) (continuation.Approval, error) {
	var out continuation.Approval
	err := s.withLock(func() error {
		// Validate before creating custody. A syntactic placeholder key does not grant
		// authority; the actual public key is inserted exclusively from private custody.
		a.PublicKey = base64.StdEncoding.EncodeToString(make([]byte, ed25519.PublicKeySize))
		a.Signature = ""
		if _, err := continuation.Payload(a); err != nil {
			return err
		}
		path, err := s.continuationPath(a.ID)
		if err != nil {
			return err
		}
		key, err := s.continuationKey(true)
		if err != nil {
			return err
		}
		public := key.Public().(ed25519.PublicKey)
		a.PublicKey = base64.StdEncoding.EncodeToString(public)
		preimage, err := continuation.Payload(a)
		if err != nil {
			return err
		}
		a.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, preimage))
		wire, err := json.Marshal(a)
		if err != nil {
			return err
		}
		if err = s.secureStoreDirectory(filepath.Dir(path), true); err != nil {
			return err
		}
		err = writeBytesCreateOnly(path, wire)
		if err != nil && !errors.Is(err, ErrAlreadyExists) {
			return err
		}
		stored, err := protectedContinuationRead(path)
		if err != nil {
			return err
		}
		out, err = continuation.Decode(stored)
		if err != nil {
			return err
		}
		if err = continuation.Verify(out, public); err != nil {
			return err
		}
		if !bytes.Equal(wire, stored) {
			return continuation.ErrConflict
		}
		return nil
	})
	return out, err
}

func (s *Store) ReadDoerContinuation(id string) (continuation.Approval, error) {
	var out continuation.Approval
	err := s.withLock(func() error {
		path, err := s.continuationPath(id)
		if err != nil {
			return err
		}
		if err = s.secureStoreDirectory(filepath.Dir(path), false); err != nil {
			return err
		}
		key, err := s.continuationKey(false)
		if err != nil {
			return err
		}
		wire, err := protectedContinuationRead(path)
		if err != nil {
			return err
		}
		out, err = continuation.Decode(wire)
		if err != nil {
			return err
		}
		if out.ID != id {
			return continuation.ErrInvalid
		}
		return continuation.Verify(out, key.Public().(ed25519.PublicKey))
	})
	return out, err
}
