// Package implementation implements the bounded, controller-verified action
// kernel. Go tests execute operator-authorized repository code, not a sandbox.
package implementation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/loop"
)

const maxBytes = 8 << 20

type Edit struct {
	Path    string `json:"path"`
	Content []byte `json:"content"`
}
type Request struct {
	InputObserver     func(InputObservation) error `json:"-"`
	Contract          loop.VerifiedImplementation
	RepositoryContext *CollectedRepositoryContext `json:",omitempty"`
	PassID            string
	PreviousCheck     []byte
	PreviousDiagnosis []byte
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
	Input              *InputEvidence `json:"input,omitempty"`
	ContextIdentity    string         `json:"context_identity,omitempty"`
	ContextStateDigest string         `json:"context_state_digest,omitempty"`
	ID                 string         `json:"id"`
	WorkspaceDigest    string         `json:"workspace_digest"`
	OutputDigest       string         `json:"output_digest"`
	Passed             bool           `json:"passed"`
}
type Record struct {
	ManagedInput       bool    `json:"managed_input,omitempty"`
	PredecessorRun     string  `json:"predecessor_run,omitempty"`
	PredecessorHistory string  `json:"predecessor_history,omitempty"`
	ConsumedBefore     uint16  `json:"consumed_before,omitempty"`
	ContextDigest      string  `json:"context_digest,omitempty"`
	RunID              string  `json:"run_id"`
	ContractDigest     string  `json:"contract_digest"`
	State              string  `json:"state"`
	Passes             []Pass  `json:"passes"`
	Completion         string  `json:"completion,omitempty"`
	Stages             []Stage `json:"stages,omitempty"`
	ReportError        string  `json:"report_error,omitempty"`
}
type Executor struct {
	RequireInputEvidence bool
	Continuation         *Continuation
	PassLimit            uint8 // zero retains the exact contract budget
	DB                   Store
	GoBinary             string
	Proposer             Proposer
	Admit                Admit
	Decision             Decision
	Diagnosis            Diagnostician
	Reporter             CompletionReporter
}

func digest(b []byte) string     { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func recordKey(id string) []byte { return []byte("implementation/run/" + digest([]byte(id))) }
func blobKey(id string) []byte   { return []byte("implementation/blob/" + id) }

// Store is opaque controller custody. Create must atomically reject existing keys.
// Implementations own engine transactions and map absent reads to ErrNotFound.
type Store interface {
	Get([]byte) ([]byte, error)
	Put([]byte, []byte) error
	Create([]byte, []byte) error
}

var ErrNotFound = errors.New("implementation record not found")

func (e *Executor) save(r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return e.DB.Put(recordKey(r.RunID), b)
}
func (e *Executor) blob(b []byte) (string, error) {
	d := digest(b)
	return d, e.DB.Put(blobKey(d), b)
}
func (e *Executor) loadBlob(d string) ([]byte, error) {
	b, err := e.DB.Get(blobKey(d))
	if err == nil && digest(b) != d {
		err = errors.New("evidence digest mismatch")
	}
	return b, err
}
func (e *Executor) Read(id string) (Record, error) {
	var r Record
	b, err := e.DB.Get(recordKey(id))
	if err == nil {
		err = json.Unmarshal(b, &r)
	}
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
	c = cloneImplementation(c)
	if e.DB == nil || e.Proposer == nil || e.Admit == nil || !filepath.IsAbs(e.GoBinary) || id == "" || len(id) > 255 {
		return r, errors.New("controller dependencies and exact run identity required")
	}
	if err := Preflight(c, e.GoBinary); err != nil {
		return r, err
	}
	doer := c.DecisionMode == "doer.v1"
	reporting, reports := e.Proposer.(ReportingProposer)
	if doer && (e.Decision == nil || e.Diagnosis == nil || !reports) {
		return r, errors.New("doer decision, diagnosis and reporting proposer required")
	}
	cd, err := c.Digest()
	if err != nil {
		return r, err
	}
	resolved, err := filepath.EvalSymlinks(c.Workspace)
	if err != nil || resolved != c.Workspace {
		return r, errors.New("workspace must be a resolved operator-authorized directory")
	}
	unlock, err := lockWorkspace(c.Workspace)
	if err != nil {
		return r, err
	}
	defer unlock()
	var continuedInitial, continuedCurrent *CollectedRepositoryContext
	if e.Continuation != nil {
		continuedInitial, continuedCurrent, err = e.continuationContext(id, c)
		if err != nil {
			return r, err
		}
	}
	if old, x := e.Read(id); x == nil {
		if old.ContractDigest != cd {
			return old, errors.New("reserved contract mismatch")
		}
		if old.State == "running" {
			old.State = "interrupted"
			if x = e.save(old); x != nil {
				return old, x
			}
		}
		return old, errors.New("run already reserved; budget cannot reset")
	} else if !errors.Is(x, ErrNotFound) {
		return r, x
	}
	admissionErr := e.admit(ctx, "begin")
	r = Record{RunID: id, ContractDigest: cd, State: "running", Passes: []Pass{}, ManagedInput: e.RequireInputEvidence}
	if t := e.Continuation; t != nil {
		// Atomic predecessor slot survives unknown outcomes; no second successor.
		if err = e.DB.Create([]byte("implementation/successor/"+digest([]byte(t.predecessor))), []byte(id)); err != nil {
			return r, err
		}
		r.PredecessorRun, r.PredecessorHistory, r.ConsumedBefore = t.predecessor, t.history, t.consumed
	}
	wire, err := json.Marshal(r)
	if err == nil {
		err = e.DB.Create(recordKey(id), wire)
	}
	if err != nil {
		return r, err
	}
	if admissionErr != nil {
		return e.stop(r, admissionErr)
	}
	var initial, expected *CollectedRepositoryContext
	if c.RepositoryContext != nil {
		if e.Continuation != nil {
			initial = continuedInitial
		} else {
			initial, err = CollectRepositoryContext(ctx, c, RepositorySourceDigest(c.RepositoryContext))
		}
		if err != nil {
			return e.stop(r, err)
		}
		for _, path := range c.WritableFiles {
			found := false
			for _, f := range initial.Files {
				if f.Path == path {
					found = true
				}
			}
			if !found {
				return e.stop(r, errors.New("repository context missing writable preimage"))
			}
		}
		expected = cloneContext(initial)
		if e.Continuation != nil {
			expected = cloneContext(continuedCurrent)
		}
		r.ContextDigest, err = e.contextBlob(initial)
		if err != nil {
			return e.stop(r, err)
		}
		if err = e.save(r); err != nil {
			return r, err
		}
	}
	var previous []byte
	var previousDiagnosis []byte
	if doer {
		if err = e.admit(ctx, "gate"); err != nil {
			return e.stop(r, err)
		}
		if err = verifyContext(ctx, c, expected); err != nil {
			return e.stop(r, err)
		}
		gate, x := e.Decision.Gate(ctx, "Task: "+c.Task+"\nExpected result: "+c.Acceptance)
		if x != nil {
			return e.stop(r, x)
		}
		if err = e.stage(&r, "", "gate", gate); err != nil {
			return r, err
		}
		if !gate.Specified || !gate.ResultDefined {
			r.State = "needs_input"
			err = e.save(r)
			if err == nil {
				r, err = e.completeReport(ctx, r)
			}
			return r, err
		}
	}
	limit := c.MaxPasses
	if e.Continuation != nil {
		limit = uint8(e.Continuation.remaining)
	}
	if e.PassLimit > 0 && e.PassLimit < limit {
		limit = e.PassLimit
	}
	for p := uint8(1); p <= limit; p++ {
		var failures []string
		r.Passes = append(r.Passes, Pass{ID: fmt.Sprintf("%s/pass/%d", id, p)})
		if err = e.save(r); err != nil {
			return r, err
		}
		if err = e.admit(ctx, "implement"); err != nil {
			return e.stop(r, err)
		}
		if err = verifyContext(ctx, c, expected); err != nil {
			return e.stop(r, err)
		}
		// Never share the controller's slice backing arrays with an untrusted
		// proposal adapter; its request cannot rewrite the immutable policy.
		proposalContract := cloneImplementation(c)
		passID := r.Passes[len(r.Passes)-1].ID
		request := Request{Contract: proposalContract, RepositoryContext: cloneContext(initial), PassID: passID, PreviousCheck: append([]byte(nil), previous...), PreviousDiagnosis: append([]byte(nil), previousDiagnosis...)}
		if r.ManagedInput {
			request.InputObserver = e.inputObserver(&r, c, passID)
		}
		var edits []Edit
		var report string
		var x error
		if doer {
			var proposal Proposal
			proposal, x = reporting.ProposeReport(ctx, request)
			if x == nil {
				edits, report = proposal.Edits, proposal.Report
			}
		} else {
			edits, x = e.Proposer.Propose(ctx, request)
		}
		if x != nil {
			return e.stop(r, x)
		}
		if doer {
			if report == "" || len(report) > 65536 {
				return e.stop(r, errors.New("bounded implementer report required"))
			}
			if err = e.stage(&r, passID, "implement", report); err != nil {
				return r, err
			}
		}
		if err = verifyContext(ctx, c, expected); err != nil {
			return e.stop(r, err)
		}
		if err = e.apply(ctx, c, edits, expected); err != nil {
			return e.stop(r, err)
		}
		judged := true
		var judgment Judgment
		if doer {
			if err = e.admit(ctx, "judgment"); err != nil {
				return e.stop(r, err)
			}
			judgment, x = e.Decision.Judge(ctx, "Task: "+c.Task+"\nExpected result: "+c.Acceptance, report)
			if x != nil {
				return e.stop(r, x)
			}
			if err = e.stage(&r, passID, "judgment", judgment); err != nil {
				return r, err
			}
			judged = judgment.Passed()
		}
		before, x := contractSnapshot(c)
		if x != nil {
			return e.stop(r, x)
		}
		if err = e.admit(ctx, "check"); err != nil {
			return e.stop(r, err)
		}
		if err = verifyContext(ctx, c, expected); err != nil {
			return e.stop(r, err)
		}
		output, passed, x := e.checkCandidate(ctx, c, expected)
		if x != nil {
			return e.stop(r, x)
		}
		after, x := contractSnapshot(c)
		if x != nil {
			return e.stop(r, x)
		}
		if digest(before) != digest(after) {
			return e.stop(r, errors.New("workspace changed during verification"))
		}
		if err = verifyContext(ctx, c, expected); err != nil {
			return e.stop(r, err)
		}
		wd, x := e.blob(after)
		if x != nil {
			return e.stop(r, x)
		}
		od, x := e.blob(output)
		if x != nil {
			return e.stop(r, x)
		}
		if doer {
			if err = e.stage(&r, passID, "check", struct {
				OutputDigest string `json:"output_digest"`
				Passed       bool   `json:"passed"`
			}{od, passed}); err != nil {
				return r, err
			}
		}
		input := r.Passes[len(r.Passes)-1].Input
		r.Passes[len(r.Passes)-1] = Pass{ID: passID, WorkspaceDigest: wd, OutputDigest: od, Passed: passed && judged, Input: input}
		if initial != nil {
			r.Passes[len(r.Passes)-1].ContextIdentity = initial.Identity
			r.Passes[len(r.Passes)-1].ContextStateDigest, err = e.contextBlob(expected)
			if err != nil {
				return e.stop(r, err)
			}
		}
		if err = e.save(r); err != nil {
			return r, err
		}
		if passed && judged {
			if err = e.admit(ctx, "complete"); err != nil {
				return e.stop(r, err)
			}
			// Reload persisted receipts and content, rather than trusting in-memory PASS.
			if err = e.Revalidate(id, c); err != nil {
				return e.stop(r, err)
			}
			r.State = "succeeded"
			err = e.save(r)
			if err == nil && doer {
				r, err = e.completeReport(ctx, r)
			}
			return r, err
		}
		previous = closedCheckDiagnostics(output, passed, c.Policy.RequiredTests)
		if doer {
			if !judged {
				failures = append(failures, "judgment rejected implementer report")
			}
			if !passed {
				failures = append(failures, "independent Go test failed")
			}
			if p == limit {
				break
			}
			if err = e.admit(ctx, "diagnosis"); err != nil {
				return e.stop(r, err)
			}
			if err = verifyContext(ctx, c, expected); err != nil {
				return e.stop(r, err)
			}
			previousDiagnosis, x = e.Diagnosis.Diagnose(ctx, DiagnosisRequest{Task: c.Task, Acceptance: c.Acceptance, PassID: passID, Report: report, Judgment: judgment, Check: append([]byte(nil), previous...), Failures: append([]string(nil), failures...)})
			if x != nil {
				return e.stop(r, x)
			}
			if len(previousDiagnosis) > 65536 {
				return e.stop(r, errors.New("diagnosis exceeded limit"))
			}
			if err = e.stage(&r, passID, "diagnosis", string(previousDiagnosis)); err != nil {
				return r, err
			}
		}
	}
	r, err = e.stop(r, errors.New("verification budget exhausted"))
	if doer {
		var reportErr error
		r, reportErr = e.completeReport(ctx, r)
		if reportErr != nil {
			return r, reportErr
		}
	}
	return r, err
}

func (e *Executor) completeReport(ctx context.Context, r Record) (Record, error) {
	if e.Reporter == nil {
		return r, nil
	}
	terminal := r // The terminal record was saved before optional reporting.
	if err := e.admit(ctx, "completion_report"); err != nil {
		r.ReportError = "completion report admission unavailable"
	} else {
		completion, err := e.Reporter.Report(ctx, r)
		if err != nil || len(completion) > 4096 || completion == "" {
			r.ReportError = "completion report failed"
		} else {
			r.Completion = completion
		}
	}
	if err := e.save(r); err != nil {
		// Optional narration cannot turn a verified result into an evidence-free
		// Queue disposition. The caller independently reloads the terminal
		// record and native evidence before committing success.
		return terminal, nil
	}
	return r, nil
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
	if err = e.revalidateContext(c, r); err != nil {
		return err
	}
	if err = e.revalidateInputs(c, r, true); err != nil {
		return err
	}
	if c.DecisionMode == "doer.v1" {
		if len(r.Stages) < 4 || r.Stages[0].Name != "gate" || r.Stages[0].PassID != "" {
			return errors.New("gate stage missing")
		}
		for _, stage := range r.Stages {
			if stage.Name != "gate" && stage.Name != "implement" && stage.Name != "judgment" && stage.Name != "check" && stage.Name != "diagnosis" && stage.Name != "input_prepared" && stage.Name != "input_delivered" {
				return errors.New("unknown decision stage")
			}
			if _, err := e.StageOutput(id, stage); err != nil {
				return err
			}
		}
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
	output, err := e.loadBlob(last.OutputDigest)
	if err != nil {
		return err
	}
	if !requiredTestsPassed(output, c.Policy.RequiredTests) {
		return errors.New("required test evidence missing")
	}
	if !last.Passed {
		return errors.New("controller check did not pass")
	}
	b, err := contractSnapshot(c)
	if err != nil {
		return err
	}
	if digest(b) != last.WorkspaceDigest {
		return errors.New("verified workspace was modified")
	}
	return nil
}

// Output reloads the exact verified workspace artifact from controller custody.
func (e *Executor) Output(id string, c loop.VerifiedImplementation) ([]byte, error) {
	if err := e.Revalidate(id, c); err != nil {
		return nil, err
	}
	r, err := e.Read(id)
	if err != nil {
		return nil, err
	}
	return e.loadBlob(r.Passes[len(r.Passes)-1].WorkspaceDigest)
}

func (e *Executor) apply(ctx context.Context, c loop.VerifiedImplementation, edits []Edit, states ...*CollectedRepositoryContext) error {
	var expected *CollectedRepositoryContext
	if len(states) != 0 {
		expected = states[0]
	}
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
	if _, err := contractSnapshot(c); err != nil {
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
		if err = verifyContext(ctx, c, expected); err != nil {
			return err
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return err
		}
		temp := filepath.Join(filepath.Dir(v.Path), ".aegis-edit-"+hex.EncodeToString(nonce[:]))
		f, x := root.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		defer root.Remove(temp)
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
		if err = root.Rename(temp, v.Path); err != nil {
			return err
		}
		advanceContext(c, expected, []Edit{v})
		if err = verifyContext(ctx, c, expected); err != nil {
			return err
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
	var expected *CollectedRepositoryContext
	if c.RepositoryContext != nil {
		var err error
		expected, err = CollectRepositoryContext(ctx, c, RepositorySourceDigest(c.RepositoryContext))
		if err != nil {
			return nil, false, err
		}
	}
	return e.checkCandidate(ctx, c, expected)
}

func (e *Executor) checkCandidate(ctx context.Context, c loop.VerifiedImplementation, expected *CollectedRepositoryContext) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.Policy.TimeoutSeconds)*time.Second)
	defer cancel()
	home, err := os.MkdirTemp("", "aegis-go-check-")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(home)
	workspace := c.Workspace
	if c.RepositoryContext != nil {
		workspace = filepath.Join(home, "workspace")
		if err = materializeCheckerContext(ctx, c, expected, workspace); err != nil {
			return nil, false, err
		}
	}
	args := []string{"test", "-json", "-count=1", "-p=1", "-timeout=" + fmt.Sprintf("%ds", c.Policy.TimeoutSeconds)}
	args = append(args, c.Policy.Packages...)
	cmd := exec.CommandContext(ctx, e.GoBinary, args...)
	if err = configureProcess(cmd); err != nil {
		return nil, false, err
	}
	defer killProcessGroup(cmd)
	cmd.Dir = workspace
	cmd.Env = []string{"PATH=" + filepath.Dir(e.GoBinary), "HOME=" + home, "GOCACHE=" + filepath.Join(home, "cache"), "GOMODCACHE=" + filepath.Join(home, "modules"), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0", "GOMAXPROCS=2"}
	cmd.WaitDelay = time.Second
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	if c.RepositoryContext != nil {
		if x := verifyContext(ctx, c, expected); x != nil {
			return nil, false, x
		}
	}
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
	return out.bytes, err == nil && requiredTestsPassed(out.bytes, c.Policy.RequiredTests), nil
}

func snapshot(root string) ([]byte, error) {
	type file struct {
		Path    string `json:"path"`
		Content []byte `json:"content"`
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	files := []file{}
	total := 0
	err = fs.WalkDir(dir.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := p
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
		f, x := dir.Open(rel)
		if x != nil {
			return x
		}
		defer f.Close()
		info, x := f.Stat()
		if x != nil {
			return x
		}
		if !info.Mode().IsRegular() || !singleLink(info) {
			return errors.New("workspace requires single-link regular files")
		}
		if info.Size() > maxBytes || total+int(info.Size()) > maxBytes || len(files) >= 4096 {
			return errors.New("workspace exceeds content bound")
		}
		b, x := io.ReadAll(io.LimitReader(f, int64(maxBytes-total)+1))
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
