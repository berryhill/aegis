// Package implementation implements the bounded, controller-verified action
// kernel. Go tests execute operator-authorized repository code, not a sandbox.
package implementation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/dgraph-io/badger/v4"
)

const maxBytes = 8 << 20

type Edit struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}
type Request struct {
	Contract      loop.VerifiedImplementation
	PassID        string
	PreviousCheck []byte
}
type Proposer interface {
	Propose(context.Context, Request) ([]Edit, error)
}

// Admit must freshly resolve controller authority; neither contract nor model
// output establishes it. Return a Halt to retain revoked/expired distinctions.
type Admit func(context.Context, string) error
type Halt struct{ State string }

func (h *Halt) Error() string { return h.State }

type Pass struct {
	ID              string `json:"id"`
	WorkspaceDigest string `json:"workspace_digest"`
	OutputDigest    string `json:"output_digest"`
	Passed          bool   `json:"passed"`
}
type Record struct {
	RunID          string `json:"run_id"`
	ContractDigest string `json:"contract_digest"`
	State          string `json:"state"`
	Passes         []Pass `json:"passes"`
}
type Executor struct {
	DB       *badger.DB
	GoBinary string
	Proposer Proposer
	Admit    Admit
}

func digest(b []byte) string     { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func recordKey(id string) []byte { return []byte("implementation/run/" + digest([]byte(id))) }
func blobKey(id string) []byte   { return []byte("implementation/blob/" + id) }
func putJSON(txn *badger.Txn, k []byte, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return txn.Set(k, b)
}
func (e *Executor) save(r Record) error {
	return e.DB.Update(func(t *badger.Txn) error { return putJSON(t, recordKey(r.RunID), r) })
}
func (e *Executor) blob(b []byte) (string, error) {
	d := digest(b)
	return d, e.DB.Update(func(t *badger.Txn) error { return t.Set(blobKey(d), b) })
}
func (e *Executor) loadBlob(d string) ([]byte, error) {
	var b []byte
	err := e.DB.View(func(t *badger.Txn) error {
		i, x := t.Get(blobKey(d))
		if x != nil {
			return x
		}
		b, x = i.ValueCopy(nil)
		return x
	})
	if err == nil && digest(b) != d {
		err = errors.New("evidence digest mismatch")
	}
	return b, err
}
func (e *Executor) Read(id string) (Record, error) {
	var r Record
	err := e.DB.View(func(t *badger.Txn) error {
		i, x := t.Get(recordKey(id))
		if x != nil {
			return x
		}
		return i.Value(func(b []byte) error { return json.Unmarshal(b, &r) })
	})
	return r, err
}

func (e *Executor) admit(ctx context.Context, effect string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return e.Admit(ctx, effect)
}
func (e *Executor) stop(r Record, err error) (Record, error) {
	r.State = "failed"
	var h *Halt
	switch {
	case errors.Is(err, context.Canceled):
		r.State = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		r.State = "expired"
	case errors.As(err, &h):
		switch h.State {
		case "denied", "revoked", "expired", "cancelled":
			r.State = h.State
		}
	}
	if x := e.save(r); x != nil {
		return r, x
	}
	return r, err
}

// Run reserves the run before the first effect. A repeated invocation never
// replenishes the pass budget, even after a process crash. Interrupted runs are
// retained and require explicit controller reconciliation; they are not replayed.
func (e *Executor) Run(ctx context.Context, id string, c loop.VerifiedImplementation) (Record, error) {
	var r Record
	if e.DB == nil || e.Proposer == nil || e.Admit == nil || !filepath.IsAbs(e.GoBinary) || id == "" || len(id) > 255 {
		return r, errors.New("controller dependencies and exact run identity required")
	}
	cd, err := c.Digest()
	if err != nil {
		return r, err
	}
	resolved, err := filepath.EvalSymlinks(c.Workspace)
	if err != nil || resolved != c.Workspace {
		return r, errors.New("workspace must be a resolved operator-authorized directory")
	}
	if err = e.admit(ctx, "begin"); err != nil {
		return r, err
	}
	r = Record{RunID: id, ContractDigest: cd, State: "running", Passes: []Pass{}}
	err = e.DB.Update(func(t *badger.Txn) error {
		_, x := t.Get(recordKey(id))
		if x == nil {
			return errors.New("run already reserved; budget cannot reset")
		}
		if !errors.Is(x, badger.ErrKeyNotFound) {
			return x
		}
		return putJSON(t, recordKey(id), r)
	})
	if err != nil {
		return r, err
	}
	var previous []byte
	for p := uint8(1); p <= c.MaxPasses; p++ {
		r.Passes = append(r.Passes, Pass{ID: fmt.Sprintf("%s/pass/%d", id, p)})
		if err = e.save(r); err != nil {
			return r, err
		}
		if err = e.admit(ctx, "implement"); err != nil {
			return e.stop(r, err)
		}
		// Never share the controller's slice backing arrays with an untrusted
		// proposal adapter; its request cannot rewrite the immutable policy.
		proposalContract := c
		proposalContract.WritableFiles = append([]string(nil), c.WritableFiles...)
		proposalContract.Policy.Packages = append([]string(nil), c.Policy.Packages...)
		edits, x := e.Proposer.Propose(ctx, Request{Contract: proposalContract, PassID: r.Passes[len(r.Passes)-1].ID, PreviousCheck: append([]byte(nil), previous...)})
		if x != nil {
			return e.stop(r, x)
		}
		if err = e.apply(ctx, c, edits); err != nil {
			return e.stop(r, err)
		}
		before, x := snapshot(c.Workspace)
		if x != nil {
			return e.stop(r, x)
		}
		if err = e.admit(ctx, "check"); err != nil {
			return e.stop(r, err)
		}
		output, passed, x := e.check(ctx, c)
		if x != nil {
			return e.stop(r, x)
		}
		after, x := snapshot(c.Workspace)
		if x != nil {
			return e.stop(r, x)
		}
		if digest(before) != digest(after) {
			return e.stop(r, errors.New("workspace changed during verification"))
		}
		wd, x := e.blob(after)
		if x != nil {
			return e.stop(r, x)
		}
		od, x := e.blob(output)
		if x != nil {
			return e.stop(r, x)
		}
		r.Passes[len(r.Passes)-1] = Pass{ID: r.Passes[len(r.Passes)-1].ID, WorkspaceDigest: wd, OutputDigest: od, Passed: passed}
		if err = e.save(r); err != nil {
			return r, err
		}
		if passed {
			if err = e.admit(ctx, "complete"); err != nil {
				return e.stop(r, err)
			}
			// Reload persisted receipts and content, rather than trusting in-memory PASS.
			if err = e.Revalidate(id, c); err != nil {
				return e.stop(r, err)
			}
			r.State = "succeeded"
			err = e.save(r)
			return r, err
		}
		previous = output
	}
	return e.stop(r, errors.New("verification budget exhausted"))
}

// Revalidate is also the persistence-completion gate for adapters integrating
// this kernel: exact contract, stored check output and current workspace match.
func (e *Executor) Revalidate(id string, c loop.VerifiedImplementation) error {
	r, err := e.Read(id)
	if err != nil {
		return err
	}
	cd, err := c.Digest()
	if err != nil {
		return err
	}
	if r.RunID != id || r.ContractDigest != cd || (r.State != "running" && r.State != "succeeded") || len(r.Passes) == 0 || len(r.Passes) > int(c.MaxPasses) {
		return errors.New("completion binding mismatch")
	}
	for n, p := range r.Passes {
		if p.ID != fmt.Sprintf("%s/pass/%d", id, n+1) {
			return errors.New("pass identity mismatch")
		}
		if _, err = e.loadBlob(p.OutputDigest); err != nil {
			return err
		}
		if _, err = e.loadBlob(p.WorkspaceDigest); err != nil {
			return err
		}
	}
	last := r.Passes[len(r.Passes)-1]
	if !last.Passed {
		return errors.New("controller check did not pass")
	}
	b, err := snapshot(c.Workspace)
	if err != nil {
		return err
	}
	if digest(b) != last.WorkspaceDigest {
		return errors.New("verified workspace was modified")
	}
	return nil
}

func (e *Executor) apply(ctx context.Context, c loop.VerifiedImplementation, edits []Edit) error {
	if len(edits) == 0 || len(edits) > len(c.WritableFiles) {
		return errors.New("bounded nonempty source edits required")
	}
	allowed := map[string]bool{}
	for _, p := range c.WritableFiles {
		allowed[p] = true
	}
	seen := map[string]bool{}
	size := 0
	for _, v := range edits {
		size += len(v.Content)
		if !allowed[v.Path] || seen[v.Path] || size > maxBytes {
			return errors.New("edit outside authorized source policy")
		}
		seen[v.Path] = true
	}
	// Reject symlinks/nonregular files before writes. os.Root additionally prevents
	// a concurrent path replacement from redirecting a write outside the root.
	if _, err := snapshot(c.Workspace); err != nil {
		return err
	}
	root, err := os.OpenRoot(c.Workspace)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, v := range edits {
		if err = e.admit(ctx, "write:"+v.Path); err != nil {
			return err
		}
		f, x := root.OpenFile(v.Path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if x != nil {
			return x
		}
		_, x = f.Write(v.Content)
		if x == nil {
			x = f.Sync()
		}
		closeErr := f.Close()
		if x != nil {
			return x
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

type boundedOutput struct {
	bytes    []byte
	overflow bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	space := maxBytes - len(b.bytes)
	if n > space {
		b.overflow = true
		p = p[:space]
	}
	b.bytes = append(b.bytes, p...)
	return n, nil
}
func (e *Executor) check(ctx context.Context, c loop.VerifiedImplementation) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.Policy.TimeoutSeconds)*time.Second)
	defer cancel()
	home, err := os.MkdirTemp("", "aegis-go-check-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(home)
	args := []string{"test", "-count=1", "-p=1", "-timeout=" + fmt.Sprintf("%ds", c.Policy.TimeoutSeconds)}
	args = append(args, c.Policy.Packages...)
	cmd := exec.CommandContext(ctx, e.GoBinary, args...)
	cmd.Dir = c.Workspace
	cmd.Env = []string{"PATH=" + filepath.Dir(e.GoBinary), "HOME=" + home, "GOCACHE=" + filepath.Join(home, "cache"), "GOMODCACHE=" + filepath.Join(home, "modules"), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0", "GOMAXPROCS=2"}
	cmd.WaitDelay = time.Second
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if ctx.Err() != nil {
		return nil, false, ctx.Err()
	}
	if out.overflow {
		return nil, false, errors.New("check output exceeded limit")
	}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, false, err
		}
	}
	return out.bytes, err == nil, nil
}

func snapshot(root string) ([]byte, error) {
	type file struct {
		Path    string `json:"path"`
		Content []byte `json:"content"`
	}
	files := []file{}
	total := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, x := filepath.Rel(root, p)
		if x != nil {
			return x
		}
		if rel == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return errors.New("workspace contains a nonregular file")
		}
		if strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("workspace escape")
		}
		info, x := d.Info()
		if x != nil {
			return x
		}
		if info.Size() > maxBytes || total+int(info.Size()) > maxBytes || len(files) >= 4096 {
			return errors.New("workspace exceeds content bound")
		}
		b, x := os.ReadFile(p)
		if x != nil {
			return x
		}
		total += len(b)
		if total > maxBytes {
			return errors.New("workspace exceeds content bound")
		}
		files = append(files, file{rel, b})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(files)
}
