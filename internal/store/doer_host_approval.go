package store

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/hostapproval"
)

// A dedicated signer lives outside ordinary approval facts. Resolve NEVER
// initializes a missing key. Audit and continuation keys are not used.
func (s *Store) hostApprovalKey(create bool) (ed25519.PrivateKey, error) {
	dir := s.checkpointRoot
	resolved, e := filepath.EvalSymlinks(dir)
	if e != nil || resolved != dir {
		return nil, hostapproval.ErrDenied
	}
	info, e := os.Lstat(dir)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, hostapproval.ErrDenied
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		return nil, hostapproval.ErrDenied
	}
	name := filepath.Join(dir, "doer-host-write-ed25519.key")
	if create {
		if _, e = os.Lstat(name); errors.Is(e, os.ErrNotExist) {
			_, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return nil, err
			}
			if err = writeBytesCreateOnly(name, key); err != nil && !errors.Is(err, ErrAlreadyExists) {
				return nil, err
			}
		}
	}
	fd, e := syscall.Open(name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, hostapproval.ErrDenied
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	info, e = f.Stat()
	if e != nil {
		return nil, hostapproval.ErrDenied
	}
	st, ok = info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || st.Nlink != 1 || st.Uid != uint32(os.Geteuid()) {
		return nil, hostapproval.ErrDenied
	}
	b, e := io.ReadAll(io.LimitReader(f, ed25519.PrivateKeySize+1))
	if e != nil || len(b) != ed25519.PrivateKeySize {
		return nil, hostapproval.ErrDenied
	}
	key := ed25519.PrivateKey(b)
	if !bytes.Equal(key, ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])) {
		return nil, hostapproval.ErrDenied
	}
	return key, nil
}

func (s *Store) secureHostApprovalDirectory(create bool) error {
	dir := filepath.Join(s.root, "doer-host-approvals")
	if err := s.secureStoreDirectory(dir, create); err != nil {
		return hostapproval.ErrDenied
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		return hostapproval.ErrDenied
	}
	for _, name := range []string{s.root, dir} {
		info, err := os.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
			return hostapproval.ErrDenied
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != uint32(os.Geteuid()) {
			return hostapproval.ErrDenied
		}
	}
	return nil
}

func hostApprovalGenerationName(slot string, generation uint64) string {
	if generation == 0 {
		return slot + ".json"
	}
	return fmt.Sprintf("%s.g%06d.json", slot, generation)
}

// Save is called only after the separate authenticated human consent boundary.
// It cannot renew on read, and never edits or prunes a signed historical fact.
func (s *Store) SaveDoerHostApproval(r hostapproval.Record) (hostapproval.Record, error) {
	return s.saveDoerHostApproval(r, writeBytesCreateOnly)
}

// The publication dependency is explicit only for deterministic fault tests;
// the exported custody boundary always uses durable create-only publication.
func (s *Store) saveDoerHostApproval(r hostapproval.Record, publish func(string, []byte) error) (hostapproval.Record, error) {
	var out hostapproval.Record
	err := s.withLock(func() error {
		dir := filepath.Join(s.root, "doer-host-approvals")
		if e := s.secureHostApprovalDirectory(true); e != nil {
			return e
		}
		// Existing history must not acquire a replacement signer if its key is lost.
		entries, e := os.ReadDir(dir)
		if e != nil {
			return e
		}
		key, e := s.hostApprovalKey(len(entries) == 0)
		if e != nil {
			return e
		}
		// Validate the exact request even when an active generation can be reused.
		if _, e = hostapproval.Sign(r, key); e != nil {
			return e
		}
		history, e := s.doerHostApprovalHistory(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, key)
		if e != nil {
			return e
		}
		if len(history) > 0 {
			last := history[len(history)-1]
			if r.ApprovedAt.Before(last.ApprovedAt) {
				return hostapproval.ErrDenied
			}
			if hostapproval.Verify(last, key.Public().(ed25519.PublicKey), r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ApprovedAt) == nil {
				// A caller presenting history metadata must match the existing generation.
				if r.Generation != 0 || r.ApprovalPreviousDigest != "" {
					request, err := hostapproval.Sign(r, key)
					if err != nil || request.SignedDigest() != last.SignedDigest() {
						return hostapproval.ErrDenied
					}
				}
				out = last
				return nil
			}
		}
		if len(history) >= hostapproval.MaxGenerations {
			return hostapproval.ErrDenied
		}
		generation := uint64(len(history))
		previous := ""
		if generation > 0 {
			previous = history[len(history)-1].SignedDigest()
		}
		if (r.Generation != 0 || r.ApprovalPreviousDigest != "") && (r.Generation != generation || r.ApprovalPreviousDigest != previous) {
			return hostapproval.ErrDenied
		}
		r.Generation = generation
		r.ApprovalPreviousDigest = previous
		signed, e := hostapproval.Sign(r, key)
		if e != nil {
			return e
		}
		raw, e := json.Marshal(signed)
		if e != nil {
			return e
		}
		slot := hostapproval.Slot(r.DeploymentID, r.OwnerID, r.Agent, r.ContractDigest)
		writeErr := publish(filepath.Join(dir, hostApprovalGenerationName(slot, generation)), raw)
		// Always reload the exact full history, including after an unknown commit
		// outcome. Never substitute a different payload/scope or append a retry.
		key, e = s.hostApprovalKey(false)
		if e != nil {
			return e
		}
		history, e = s.doerHostApprovalHistory(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, key)
		if e != nil {
			return e
		}
		if len(history) != int(generation)+1 {
			if writeErr != nil {
				return writeErr
			}
			return hostapproval.ErrDenied
		}
		out = history[generation]
		existing, _ := json.Marshal(out)
		if !bytes.Equal(existing, raw) {
			return hostapproval.ErrDenied
		}
		return nil
	})
	return out, err
}

// History uses canonical generation names, contiguous signed predecessor links,
// non-overlapping lifetimes and exact scope. Even expired facts are reverified.
// The caller holds the Store mutex and filesystem lock throughout.
func (s *Store) doerHostApprovalHistory(deployment, owner string, agent hostapproval.AgentReference, c hostapproval.Contract, key ed25519.PrivateKey) ([]hostapproval.Record, error) {
	d, e := c.Digest()
	if e != nil {
		return nil, e
	}
	dir := filepath.Join(s.root, "doer-host-approvals")
	if e = s.secureHostApprovalDirectory(false); e != nil {
		return nil, hostapproval.ErrDenied
	}
	slot := hostapproval.Slot(deployment, owner, agent, d)
	entries, e := os.ReadDir(dir)
	if e != nil {
		return nil, hostapproval.ErrDenied
	}
	names := make(map[string]bool)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), slot) {
			names[entry.Name()] = true
			if len(names) > hostapproval.MaxGenerations {
				return nil, hostapproval.ErrDenied
			}
		}
	}
	history := make([]hostapproval.Record, 0, len(names))
	for i := 0; i < len(names); i++ {
		name := hostApprovalGenerationName(slot, uint64(i))
		if !names[name] {
			return nil, hostapproval.ErrDenied
		}
		r, e := readDoerHostApprovalFile(filepath.Join(dir, name))
		if e != nil {
			return nil, e
		}
		if r.Generation != uint64(i) || hostapproval.Verify(r, key.Public().(ed25519.PublicKey), deployment, owner, agent, c, r.ApprovedAt) != nil {
			return nil, hostapproval.ErrDenied
		}
		if i > 0 && (r.ApprovalPreviousDigest != history[i-1].SignedDigest() || r.ApprovedAt.Before(history[i-1].ExpiresAt)) {
			return nil, hostapproval.ErrDenied
		}
		history = append(history, r)
	}
	return history, nil
}

func readDoerHostApprovalFile(name string) (hostapproval.Record, error) {
	fd, e := syscall.Open(name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return hostapproval.Record{}, hostapproval.ErrDenied
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 64*1024 {
		return hostapproval.Record{}, hostapproval.ErrDenied
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
		return hostapproval.Record{}, hostapproval.ErrDenied
	}
	raw, e := io.ReadAll(io.LimitReader(f, 64*1024+1))
	if e != nil {
		return hostapproval.Record{}, hostapproval.ErrDenied
	}
	return hostapproval.Decode(raw)
}

func (s *Store) loadDoerHostApproval(deployment, owner string, agent hostapproval.AgentReference, c hostapproval.Contract, now time.Time, out *hostapproval.Record) error {
	key, e := s.hostApprovalKey(false)
	if e != nil {
		return e
	}
	history, e := s.doerHostApprovalHistory(deployment, owner, agent, c, key)
	if e != nil {
		return e
	}
	if len(history) == 0 {
		return hostapproval.ErrDenied
	}
	current := history[len(history)-1]
	if e = hostapproval.Verify(current, key.Public().(ed25519.PublicKey), deployment, owner, agent, c, now); e != nil {
		return e
	}
	*out = current
	return nil
}

func (s *Store) ReadDoerHostApproval(deployment, owner string, agent hostapproval.AgentReference, c hostapproval.Contract, now time.Time) (hostapproval.Record, error) {
	var out hostapproval.Record
	e := s.withLock(func() error { return s.loadDoerHostApproval(deployment, owner, agent, c, now, &out) })
	return out, e
}
