package store

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

type Store struct {
	root           string
	checkpointRoot string
	mu             sync.Mutex
}

func Open(root string) (*Store, error) {
	return OpenWithCheckpoints(root, filepath.Join(root, "checkpoints"))
}

func OpenWithCheckpoints(root, checkpointRoot string) (*Store, error) {
	root, err := secureDirectory(root)
	if err != nil {
		return nil, err
	}
	checkpointRoot, err = secureDirectory(checkpointRoot)
	if err != nil {
		return nil, err
	}
	return &Store{root: root, checkpointRoot: checkpointRoot}, nil
}

func secureDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Lstat(abs); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("state directory must not be a symlink: %s", abs)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	if err := os.MkdirAll(abs, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(abs, 0700); err != nil {
		return "", err
	}
	return abs, nil
}
func (s *Store) Root() string           { return s.root }
func (s *Store) CheckpointRoot() string { return s.checkpointRoot }
func ID(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}
func writeAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".aegis-*")
	if err != nil {
		return err
	}
	n := f.Name()
	defer os.Remove(n)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	e := f.Close()
	if err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	if err = os.Rename(n, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// PublishProvisioned atomically creates one new artifact under the
// Aegis-owned provisioned directory. It rejects replacement, traversal, and
// pre-existing symlink components.
func (s *Store) PublishProvisioned(path string, value any) error {
	base := filepath.Join(s.root, "provisioned")
	clean := filepath.Clean(path)
	relative, err := filepath.Rel(base, clean)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return errors.New("provisioning target is outside Aegis-owned storage")
	}
	return s.withLock(func() error {
		current := base
		for _, component := range strings.Split(filepath.Dir(relative), string(os.PathSeparator)) {
			if component == "." || component == "" {
				continue
			}
			current = filepath.Join(current, component)
			info, statErr := os.Lstat(current)
			if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
				return errors.New("provisioning path contains a symlink")
			}
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				return statErr
			}
		}
		if _, statErr := os.Lstat(clean); statErr == nil {
			return errors.New("provisioning create target already exists")
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		return writeAtomic(clean, value)
	})
}

// RemoveProvisioned removes only an Aegis-owned artifact without following
// symlinked parent components, then durably synchronizes the parent directory.
func (s *Store) RemoveProvisioned(path string) error {
	base := filepath.Join(s.root, "provisioned")
	clean := filepath.Clean(path)
	relative, err := filepath.Rel(base, clean)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return errors.New("provisioning target is outside Aegis-owned storage")
	}
	return s.withLock(func() error {
		current := base
		for _, component := range strings.Split(filepath.Dir(relative), string(os.PathSeparator)) {
			if component == "." || component == "" {
				continue
			}
			current = filepath.Join(current, component)
			info, statErr := os.Lstat(current)
			if statErr != nil {
				return statErr
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("provisioning path contains a symlink")
			}
		}
		info, statErr := os.Lstat(clean)
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return errors.New("provisioning rollback target is not a regular file")
		}
		if err := os.Remove(clean); err != nil {
			return err
		}
		directory, err := os.Open(filepath.Dir(clean))
		if err != nil {
			return err
		}
		defer directory.Close()
		return directory.Sync()
	})
}

func validComponent(v string) bool {
	if v == "" || v == "." || v == ".." || filepath.Base(v) != v {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (s *Store) objectPath(kind, id string) (string, error) {
	if !validComponent(kind) || !validComponent(id) {
		return "", errors.New("invalid store path component")
	}
	return filepath.Join(s.root, kind, id+".json"), nil
}
func read(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(v); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func (s *Store) SaveCharter(c core.CanonicalCharter) error {
	if err := core.VerifyCanonical(c); err != nil {
		return fmt.Errorf("invalid canonical charter: %w", err)
	}
	if !validComponent(c.Charter.AgentID) {
		return errors.New("invalid charter agent ID")
	}
	p := filepath.Join(s.root, "charters", c.Charter.AgentID, fmt.Sprintf("%020d-%s.json", c.Charter.Revision, strings.TrimPrefix(c.Digest, "sha256:")))
	return s.withLock(func() error {
		if _, err := os.Stat(p); err == nil {
			return errors.New("immutable charter revision already exists")
		}
		matches, _ := filepath.Glob(filepath.Join(s.root, "charters", c.Charter.AgentID, fmt.Sprintf("%020d-*.json", c.Charter.Revision)))
		if len(matches) > 0 {
			return errors.New("charter revision already exists with another digest")
		}
		return writeAtomic(p, c)
	})
}
func (s *Store) GetCharter(agent string, rev uint64) (core.CanonicalCharter, error) {
	var c core.CanonicalCharter
	if !validComponent(agent) {
		return c, errors.New("invalid charter agent ID")
	}
	pattern := "*.json"
	if rev > 0 {
		pattern = fmt.Sprintf("%020d-*.json", rev)
	}
	ms, _ := filepath.Glob(filepath.Join(s.root, "charters", agent, pattern))
	sort.Strings(ms)
	if len(ms) == 0 {
		return c, os.ErrNotExist
	}
	if err := read(ms[len(ms)-1], &c); err != nil {
		return c, err
	}
	if err := core.VerifyCanonical(c); err != nil {
		return core.CanonicalCharter{}, fmt.Errorf("stored charter verification failed: %w", err)
	}
	return c, nil
}
func (s *Store) ListCharters(agent string) ([]core.CanonicalCharter, error) {
	if !validComponent(agent) {
		return nil, errors.New("invalid charter agent ID")
	}
	ms, _ := filepath.Glob(filepath.Join(s.root, "charters", agent, "*.json"))
	sort.Strings(ms)
	out := make([]core.CanonicalCharter, 0, len(ms))
	for _, p := range ms {
		var c core.CanonicalCharter
		if err := read(p, &c); err != nil {
			return nil, err
		}
		if err := core.VerifyCanonical(c); err != nil {
			return nil, fmt.Errorf("stored charter verification failed: %w", err)
		}
		out = append(out, c)
	}
	return out, nil
}
func (s *Store) ListAgents() ([]string, error) {
	es, err := os.ReadDir(filepath.Join(s.root, "charters"))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range es {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
func (s *Store) Save(kind, id string, v any) error {
	p, err := s.objectPath(kind, id)
	if err != nil {
		return err
	}
	return s.withLock(func() error { return writeAtomic(p, v) })
}
func (s *Store) Load(kind, id string, v any) error {
	p, err := s.objectPath(kind, id)
	if err != nil {
		return err
	}
	return read(p, v)
}
func (s *Store) List(kind string, fn func(json.RawMessage) error) error {
	if !validComponent(kind) {
		return errors.New("invalid store kind")
	}
	ms, _ := filepath.Glob(filepath.Join(s.root, kind, "*.json"))
	sort.Strings(ms)
	for _, p := range ms {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err = fn(b); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) withLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
	return fn()
}
func (s *Store) Update(kind, id string, fn func([]byte) (any, error)) error {
	p, err := s.objectPath(kind, id)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		v, err := fn(b)
		if err != nil {
			return err
		}
		return writeAtomic(p, v)
	})
}

// UpdateWithIntent records protected-operation intent before publishing
// consumed authority. Recovery therefore retains evidence if publication or
// the subsequent operation fails after authority consumption.
func (s *Store) UpdateWithIntent(kind, id, intentKind, intentID string, intent any, fn func([]byte) (any, error)) error {
	primaryPath, err := s.objectPath(kind, id)
	if err != nil {
		return err
	}
	intentPath, err := s.objectPath(intentKind, intentID)
	if err != nil {
		return err
	}
	return s.withLock(func() error {
		b, err := os.ReadFile(primaryPath)
		if err != nil {
			return err
		}
		updated, err := fn(b)
		if err != nil {
			return err
		}
		if err = writeAtomic(intentPath, intent); err != nil {
			return err
		}
		return writeAtomic(primaryPath, updated)
	})
}

func sanitizeMetadata(m map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		l := strings.ToLower(k)
		if strings.Contains(l, "secret") || strings.Contains(l, "token") || strings.Contains(l, "password") || strings.Contains(l, "prompt") || strings.Contains(l, "credential") {
			out[k] = "[REDACTED]"
		} else if len(v) > 512 {
			out[k] = v[:512]
		} else {
			out[k] = strings.ReplaceAll(strings.ReplaceAll(v, "\r", " "), "\n", " ")
		}
	}
	return out
}

type auditCheckpoint struct {
	Version    int       `json:"version"`
	Count      int       `json:"count"`
	LastDigest string    `json:"last_digest"`
	CreatedAt  time.Time `json:"created_at"`
	KeyID      string    `json:"key_id"`
	PublicKey  string    `json:"public_key"`
	Signature  string    `json:"signature"`
}

func checkpointPayload(c auditCheckpoint) []byte {
	c.Signature = ""
	b, _ := json.Marshal(c)
	return b
}

func (s *Store) checkpointKey() (ed25519.PrivateKey, error) {
	path := filepath.Join(s.checkpointRoot, "signing-key")
	var stored struct {
		PrivateKey string `json:"private_key"`
	}
	err := read(path, &stored)
	if err == nil {
		raw, decodeErr := base64.StdEncoding.DecodeString(stored.PrivateKey)
		if decodeErr != nil || len(raw) != ed25519.PrivateKeySize {
			return nil, errors.New("invalid audit checkpoint signing key")
		}
		return ed25519.PrivateKey(raw), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	stored.PrivateKey = base64.StdEncoding.EncodeToString(privateKey)
	if err = writeAtomic(path, stored); err != nil {
		return nil, err
	}
	return privateKey, nil
}

func (s *Store) writeCheckpoint(count int, last string) error {
	privateKey, err := s.checkpointKey()
	if err != nil {
		return err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyHash := sha256.Sum256(publicKey)
	c := auditCheckpoint{Version: 1, Count: count, LastDigest: last, CreatedAt: time.Now().UTC(), KeyID: "sha256:" + hex.EncodeToString(keyHash[:]), PublicKey: base64.StdEncoding.EncodeToString(publicKey)}
	c.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checkpointPayload(c)))
	name := fmt.Sprintf("%020d-%s.json", count, strings.TrimPrefix(last, "sha256:"))
	path := filepath.Join(s.checkpointRoot, name)
	if _, err = os.Stat(path); err == nil {
		return errors.New("audit checkpoint already exists")
	}
	return writeAtomic(path, c)
}

func (s *Store) AppendAudit(ctx context.Context, e core.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withLock(func() error {
		path := filepath.Join(s.root, "audit.jsonl")
		prev := ""
		count := 0
		if f, err := os.Open(path); err == nil {
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				var old core.AuditEvent
				if json.Unmarshal(sc.Bytes(), &old) == nil {
					prev = old.EventDigest
					count++
				}
			}
			_ = f.Close()
			if sc.Err() != nil {
				return sc.Err()
			}
		}
		e.ID = ID("evt")
		e.OccurredAt = time.Now().UTC()
		e.PreviousDigest = prev
		e.Metadata = sanitizeMetadata(e.Metadata)
		e.EventDigest = ""
		e.EventDigest = core.Digest(e)
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if _, err = f.Write(append(b, '\n')); err != nil {
			_ = f.Close()
			return err
		}
		if err = f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		return s.writeCheckpoint(count+1, e.EventDigest)
	})
}
func (s *Store) AuditEvents() ([]core.AuditEvent, error) {
	path := filepath.Join(s.root, "audit.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []core.AuditEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []core.AuditEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 1<<20)
	for sc.Scan() {
		var e core.AuditEvent
		d := json.NewDecoder(strings.NewReader(sc.Text()))
		d.DisallowUnknownFields()
		if err = d.Decode(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, sc.Err()
}
func (s *Store) VerifyAudit() error {
	es, err := s.AuditEvents()
	if err != nil {
		return err
	}
	prev := ""
	ids := map[string]bool{}
	for i, e := range es {
		if e.ID == "" || ids[e.ID] {
			return fmt.Errorf("audit event %d has missing/duplicate id", i)
		}
		ids[e.ID] = true
		if e.PreviousDigest != prev {
			return fmt.Errorf("audit event %d previous digest mismatch", i)
		}
		got := e.EventDigest
		e.EventDigest = ""
		if core.Digest(e) != got {
			return fmt.Errorf("audit event %d digest mismatch", i)
		}
		prev = got
	}
	return s.verifyLatestCheckpoint(es)
}

func (s *Store) verifyLatestCheckpoint(events []core.AuditEvent) error {
	paths, err := filepath.Glob(filepath.Join(s.checkpointRoot, "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		if len(events) == 0 {
			return nil
		}
		return errors.New("audit chain has no retained signed checkpoint")
	}
	var c auditCheckpoint
	if err = read(paths[len(paths)-1], &c); err != nil {
		return fmt.Errorf("read audit checkpoint: %w", err)
	}
	if c.Version != 1 || c.Count <= 0 || c.Count > len(events) || c.KeyID == "" {
		return errors.New("audit checkpoint metadata is invalid")
	}
	publicKey, err := base64.StdEncoding.DecodeString(c.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return errors.New("audit checkpoint public key is invalid")
	}
	keyHash := sha256.Sum256(publicKey)
	if c.KeyID != "sha256:"+hex.EncodeToString(keyHash[:]) {
		return errors.New("audit checkpoint key identifier mismatch")
	}
	signature, err := base64.StdEncoding.DecodeString(c.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), checkpointPayload(c), signature) {
		return errors.New("audit checkpoint signature is invalid")
	}
	if events[c.Count-1].EventDigest != c.LastDigest {
		return errors.New("audit chain does not contain retained checkpoint head")
	}
	return nil
}
