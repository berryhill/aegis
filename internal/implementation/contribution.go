package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/loop"
	"golang.org/x/mod/modfile"
)

// ContributionRoleIdentity is emitted by the controller's separately admitted
// runtime, not parsed from model output. The tester receives no edit port.
type ContributionRoleIdentity struct {
	AgentDigest      string `json:"agent_digest"`
	CharterDigest    string `json:"charter_digest"`
	AuthorityID      string `json:"authority_id"`
	AuthorityDigest  string `json:"authority_digest"`
	SessionID        string `json:"session_id"`
	MandateID        string `json:"mandate_id"`
	RuntimeSessionID string `json:"runtime_session_id"`
	TurnID           string `json:"turn_id"`
}
type ContributionCandidate struct {
	Contract     string `json:"contract"`
	QueueAttempt string `json:"queue_attempt"`
	Pass         uint8  `json:"pass"`
	Base         string `json:"base"`
	// HeadCommit and HeadTree are repository anchors, NOT the dirty candidate.
	// Source.Digest is the complete tested candidate identity (including ignored files).
	CandidateCommit string                   `json:"candidate_commit"`
	CandidateTree   string                   `json:"candidate_tree"`
	HeadCommit      string                   `json:"head_commit"`
	HeadTree        string                   `json:"head_tree"`
	Index           string                   `json:"index"`
	Source          SourceInventory          `json:"source"`
	Executable      string                   `json:"executable"`
	Toolchain       string                   `json:"toolchain"`
	Policy          string                   `json:"policy"`
	Builder         ContributionRoleIdentity `json:"builder"`
	Digest          string                   `json:"digest"`
}
type ContributionCheck struct {
	Review        string                 `json:"review,omitempty"`
	Outcome       string                 `json:"outcome"`
	OutputDigest  string                 `json:"output_digest"`
	RequiredTests []loop.RequiredGoTest  `json:"required_tests"`
	Criteria      []CriterionObservation `json:"criteria,omitempty"`
}
type ContributionPass struct {
	Candidate ContributionCandidate    `json:"candidate"`
	Tester    ContributionRoleIdentity `json:"tester"`
	Check     ContributionCheck        `json:"check"`
}
type ContributionRecord struct {
	Frozen          *ContributionCandidate           `json:"frozen,omitempty"`
	FrozenToolchain string                           `json:"frozen_toolchain"`
	RunID           string                           `json:"run_id"`
	Contract        string                           `json:"contract"`
	QueueAttempt    string                           `json:"queue_attempt"`
	State           string                           `json:"state"`
	Policy          loop.ContributionExecutionPolicy `json:"policy"`
	Consumed        uint8                            `json:"consumed"`
	Base            string                           `json:"base"`
	Workspace       string                           `json:"workspace"`
	Implementation  string                           `json:"implementation"`
	BoundaryIndex   string                           `json:"boundary_index"`
	BoundarySource  string                           `json:"boundary_source"`
	Expected        *CollectedRepositoryContext      `json:"expected,omitempty"`
	Baseline        ContributionCheck                `json:"baseline"`
	Passes          []ContributionPass               `json:"passes"`
}

// ManagedContribution runs under existing controller custody/Store. It is not a
// queue, scheduler, GitHub publisher or final contribution disposition engine.
// Interrupted effects retain their reservation; only explicit quiescent phase
// boundaries permit exact same-attempt continuation. No authority renewal.
type ManagedContribution struct {
	// Checkpoint is a trusted controller port, never runtime-supplied narration.
	// It runs before the named effect; errors retain the last durable boundary.
	Checkpoint        func(context.Context, ContributionCheckpoint) error
	ContinueBuilder   func(context.Context, ContributionRoleIdentity, ContributionRoleIdentity) error
	toolchainDigest   string
	DB                Store
	GoBinary          string
	StateRoot         string
	Repository        string
	CurrentExecutable string // controller-only check input, never model-selected
	// Native owns Start/Wait and durable quiescence for every qualified command.
	Native func(context.Context, *exec.Cmd) error
	Admit  Admit
	Build  func(context.Context, Request) ([]Edit, ContributionRoleIdentity, error)
	Test   func(context.Context, ContributionCandidate, func(context.Context, ContributionRoleIdentity) error) (ContributionRoleIdentity, error)
	// Fetch is an optional trusted transport seam; origin/base validation is never skipped.
	Fetch func(context.Context, loop.ContributionExecutionPolicy) error
}

func contributionKey(id string) []byte { return []byte("contribution/execution/" + digest([]byte(id))) }
func (e *ManagedContribution) Read(id string) (ContributionRecord, error) {
	var r ContributionRecord
	b, err := e.DB.Get(contributionKey(id))
	if err == nil {
		err = json.Unmarshal(b, &r)
	}
	if err == nil {
		err = e.validateRecord(id, r)
	}
	return r, err
}
func (e *ManagedContribution) save(r ContributionRecord) error {
	b, err := json.Marshal(r)
	if err == nil {
		err = e.DB.Put(contributionKey(r.RunID), b)
	}
	return err
}
func (e *ManagedContribution) blob(b []byte) (string, error) {
	d := digest(b)
	return d, e.DB.Put(blobKey(d), b)
}
func (e *ManagedContribution) admit(ctx context.Context, effect string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return e.Admit(ctx, effect)
}

// PreflightManaged rejects unsupported profiles before executable admission.
func PreflightManaged(c loop.VerifiedImplementation, p loop.ContributionExecutionPolicy) error {
	if c.Validate() != nil || p.Validate() != nil || c.DecisionMode != "" || c.RepositoryContext == nil {
		return errors.New("unsupported managed contribution profile")
	}
	if len(c.Policy.RequiredTests) != len(p.Criteria) {
		return errors.New("exact criterion policy required")
	}
	trusted := false
	for _, f := range c.RepositoryContext.Files {
		if f.Role == "trusted_test" {
			trusted = true
		}
	}
	if !trusted {
		return errors.New("immutable trusted review and denial checks required")
	}
	for i, criterion := range p.Criteria {
		if criterion.Test != c.Policy.RequiredTests[i] {
			return errors.New("exact criterion test required")
		}
	}
	if c.Workspace == p.RepositoryPath || strings.HasPrefix(c.Workspace, p.RepositoryPath+string(os.PathSeparator)) {
		return errors.New("isolated worktree required")
	}
	for _, path := range []string{p.RepositoryPath, p.GitBinary} {
		real, err := filepath.EvalSymlinks(path)
		if err != nil || real != path {
			return errors.New("resolved managed execution paths required")
		}
	}
	return nil
}

func (e *ManagedContribution) Run(ctx context.Context, run, contract, attempt, base string, c loop.VerifiedImplementation, p loop.ContributionExecutionPolicy) (record ContributionRecord, resultErr error) {
	if e.DB == nil || e.Admit == nil || e.Build == nil || e.Test == nil || !filepath.IsAbs(e.StateRoot) || !filepath.IsAbs(e.GoBinary) || run == "" || contract == "" || attempt == "" || len(base) != 40 {
		return record, errors.New("exact managed contribution dependencies required")
	}
	if _, err := e.verifyToolchain(ctx); err != nil {
		return record, err
	}
	policyWire, marshalErr := json.Marshal(p)
	if marshalErr != nil {
		return record, marshalErr
	}
	p = loop.ContributionExecutionPolicy{}
	if err := json.Unmarshal(policyWire, &p); err != nil {
		return record, err
	}
	c = cloneImplementation(c)
	if err := PreflightManaged(c, p); err != nil {
		return record, err
	}
	// A restart may continue only an explicit quiescent phase boundary, never
	// an unknown in-flight effect. The root reservation is never released.
	if old, err := e.Read(run); err == nil {
		if old.FrozenToolchain != e.toolchainDigest {
			return old, errors.New("resumed native toolchain changed")
		}
		unlock, err := lockWorkspace(c.Workspace)
		if err != nil {
			return old, err
		}
		defer unlock()
		old, err = e.Read(run) // Re-read after exclusive custody, never use a stale boundary.
		if err != nil {
			return old, err
		}
		if err = e.resumeBoundary(ctx, old, run, contract, attempt, base, c, p); err != nil {
			return old, err
		}
		return e.runPrepared(ctx, old, c, p)
	} else if !errors.Is(err, ErrNotFound) {
		return record, err
	}
	identity, _ := json.Marshal(c)
	record = ContributionRecord{FrozenToolchain: e.toolchainDigest, RunID: run, Contract: contract, QueueAttempt: attempt, Base: base, Workspace: c.Workspace, Implementation: digest(identity), State: "running", Policy: p, Passes: []ContributionPass{}}
	b, _ := json.Marshal(record)
	if err := e.DB.Create(contributionKey(run), b); err != nil {
		return record, err
	}
	defer func() {
		if resultErr != nil && record.State != "phase_boundary" {
			record.State = "interrupted"
			_ = e.save(record)
		}
	}()
	if err := e.DB.Create([]byte("contribution/workspace/"+digest([]byte(c.Workspace))), []byte(run)); err != nil {
		return record, err
	}
	if err := e.admit(ctx, "worktree"); err != nil {
		return record, err
	}
	if _, err := os.Lstat(c.Workspace); !errors.Is(err, os.ErrNotExist) {
		return record, errors.New("unowned existing worktree denied")
	}
	if err := e.prepare(ctx, p, c.Workspace, run, base); err != nil {
		return record, err
	}
	unlock, err := lockWorkspace(c.Workspace)
	if err != nil {
		return record, err
	}
	defer unlock()
	if err = Preflight(c, e.GoBinary); err != nil {
		return record, err
	}
	expected, err := CollectRepositoryContext(ctx, c, RepositorySourceDigest(c.RepositoryContext))
	if err != nil {
		return record, err
	}
	initial, err := Inventory(ctx, c.Workspace)
	if err != nil {
		return record, err
	}
	for _, asset := range p.ReviewAssets {
		if asset.NonApplicable != "" {
			continue
		}
		found := false
		for _, f := range initial.Files {
			if f.Path == asset.Path {
				found = digest(f.Content) == asset.BaselineDigest
			}
		}
		if !found {
			return record, errors.New("approved review baseline asset missing or mismatched")
		}
	}
	if err = stdlibModule(initial); err != nil {
		return record, err
	}
	if err = os.MkdirAll(e.StateRoot, 0700); err != nil {
		return record, err
	}
	// The trusted behavior regression must really fail before the builder runs.
	baseline, _, err := e.check(ctx, c, initial)
	if err != nil {
		return record, err
	}
	record.Baseline = baseline
	baselineWire, _ := json.Marshal(baseline)
	if err = e.DB.Create([]byte("contribution/baseline/"+digest([]byte(run))), baselineWire); err != nil {
		return record, err
	}
	if baseline.Outcome != "failed" || !requiredRegressionFailed(previousBytes(e.DB, baseline.OutputDigest), c.Policy.RequiredTests) {
		return record, errors.New("trusted failing regression required")
	}
	if err = e.save(record); err != nil {
		return record, err
	}
	record.Expected = expected
	boundaryIndex, err := e.git(ctx, p, c.Workspace, "ls-files", "--stage", "-z")
	if err != nil {
		return record, err
	}
	record.BoundaryIndex = digest(boundaryIndex)
	record.BoundarySource = initial.Digest
	record.State = "phase_boundary"
	if err = e.save(record); err != nil {
		return record, err
	}
	return e.runPrepared(ctx, record, c, p)
}

func (e *ManagedContribution) runPrepared(ctx context.Context, record ContributionRecord, c loop.VerifiedImplementation, p loop.ContributionExecutionPolicy) (result ContributionRecord, resultErr error) {
	defer func() {
		if resultErr != nil && record.State != "phase_boundary" {
			record.State = "interrupted"
			_ = e.save(record)
		}
	}()
	run, contract, attempt, base := record.RunID, record.Contract, record.QueueAttempt, record.Base
	expected := cloneContext(record.Expected)
	previous, err := json.Marshal(criterionObservations(previousBytes(e.DB, record.Baseline.OutputDigest), p.Criteria))
	if len(record.Passes) > 0 {
		previous, err = json.Marshal(record.Passes[len(record.Passes)-1].Check.Criteria)
	}
	if err != nil {
		return record, err
	}
	kernel := &Executor{GoBinary: e.GoBinary, Admit: e.Admit}
	var builder ContributionRoleIdentity
	if len(record.Passes) > 0 {
		builder = record.Passes[len(record.Passes)-1].Candidate.Builder
	}
	for record.Frozen != nil || record.Consumed < c.MaxPasses {
		if err = ctx.Err(); err != nil {
			return record, err
		}
		var candidate ContributionCandidate
		var frozen SourceInventory
		if record.Frozen != nil {
			candidate = *record.Frozen
			frozen = candidate.Source
			builder = candidate.Builder
		} else {
			if err = e.checkpoint(ctx, "build", record, ""); err != nil {
				return record, err
			}
			record.State = "running"
			record.Consumed++ // Persist consumption before runtime or file effects.
			if err = e.save(record); err != nil {
				return record, err
			}
			if err = e.admit(ctx, "builder"); err != nil {
				return record, err
			}
			before, err := Inventory(ctx, c.Workspace)
			if err != nil {
				return record, err
			}
			edits, identity, err := e.Build(ctx, Request{Contract: c, RepositoryContext: cloneContext(expected), PassID: fmt.Sprintf("%s-builder-%d", run, record.Consumed), PreviousCheck: previous})
			if err != nil {
				return record, err
			}
			changed := builder.AuthorityID != "" && (identity.AgentDigest != builder.AgentDigest || identity.AuthorityID != builder.AuthorityID || identity.SessionID != builder.SessionID || identity.MandateID != builder.MandateID)
			if !validContributionRole(identity) || (changed && (e.ContinueBuilder == nil || e.ContinueBuilder(ctx, builder, identity) != nil)) {
				return record, errors.New("same independently admitted builder required")
			}
			builder = identity
			after, err := Inventory(ctx, c.Workspace)
			if err != nil || after.Digest != before.Digest {
				return record, errors.New("source changed during builder proposal")
			}
			if err = kernel.apply(ctx, c, edits, expected); err != nil {
				return record, err
			}
			frozen, err = Inventory(ctx, c.Workspace)
			if err != nil {
				return record, err
			}
			if err = onlyEdits(before, frozen, edits); err != nil {
				return record, err
			}
			if err = stdlibModule(frozen); err != nil {
				return record, err
			}
			e.CurrentExecutable = ""
			executable, err := e.build(ctx, c, p, frozen)
			if err != nil {
				return record, err
			}
			if _, err = e.verifyToolchain(ctx); err != nil {
				return record, err
			}
			commit, err := e.git(ctx, p, c.Workspace, "rev-parse", "HEAD")
			if err != nil {
				return record, err
			}
			tree, err := e.git(ctx, p, c.Workspace, "rev-parse", "HEAD^{tree}")
			if err != nil {
				return record, err
			}
			index, err := e.git(ctx, p, c.Workspace, "ls-files", "--stage", "-z")
			if err != nil {
				return record, err
			}
			candidateCommit, candidateTree, err := e.freezeGit(ctx, p, c.Workspace, base, frozen)
			if err != nil {
				return record, err
			}
			candidate = ContributionCandidate{CandidateCommit: candidateCommit, CandidateTree: candidateTree, Contract: contract, QueueAttempt: attempt, Pass: record.Consumed, Base: base, HeadCommit: strings.TrimSpace(string(commit)), HeadTree: strings.TrimSpace(string(tree)), Index: digest(index), Source: frozen, Executable: executable, Toolchain: e.toolchainDigest, Policy: contributionPolicyDigest(p), Builder: builder}
			wire, _ := json.Marshal(candidate)
			candidate.Digest = digest(wire)
			// Persist complete frozen identity before a fresh tester can start.
			wire, _ = json.Marshal(candidate)
			if err = e.DB.Create([]byte("contribution/candidate/"+candidate.Digest), wire); err != nil {
				return record, err
			}
			record.Frozen = &candidate
			// apply advanced this trusted context to the exact edited preimages.
			// Retain it with the freeze so resume cannot revert to base preimages.
			record.Expected = cloneContext(expected)
			record.BoundaryIndex = candidate.Index
			record.BoundarySource = frozen.Digest
			record.State = "phase_boundary"
			if err = e.save(record); err != nil {
				return record, err
			}
		}
		if err = e.checkpoint(ctx, "freeze", record, candidate.Digest); err != nil {
			return record, err
		}
		if err = e.admit(ctx, "tester"); err != nil {
			return record, err
		}
		if err = e.revalidate(ctx, c, p, candidate); err != nil {
			return record, err
		}
		var check ContributionCheck
		var output []byte
		var observed ContributionRoleIdentity
		called := false
		if err = e.checkpoint(ctx, "test", record, candidate.Digest); err != nil {
			return record, err
		}
		record.State = "running"
		if err = e.save(record); err != nil {
			return record, err
		}
		tester, err := e.Test(ctx, candidate, func(checkCtx context.Context, role ContributionRoleIdentity) error {
			if called || !independentContributionRole(builder, role) {
				return errors.New("independent one-shot tester capability required")
			}
			for _, prior := range record.Passes {
				if role.SessionID == prior.Tester.SessionID || role.AuthorityID == prior.Tester.AuthorityID || role.RuntimeSessionID == prior.Tester.RuntimeSessionID {
					return errors.New("tester capability replay denied")
				}
			}
			called = true
			observed = role
			if err := e.revalidate(checkCtx, c, p, candidate); err != nil {
				return err
			}
			e.CurrentExecutable = candidate.Executable
			var checkErr error
			check, output, checkErr = e.check(checkCtx, c, frozen)
			if checkErr != nil {
				return checkErr
			}
			check.Criteria = criterionObservations(output, p.Criteria)
			return e.revalidate(checkCtx, c, p, candidate)
		})
		if err != nil {
			return record, err
		}
		if !called || tester != observed || !independentContributionRole(builder, tester) {
			return record, errors.New("fresh independent tester required")
		}
		for _, pass := range record.Passes {
			if pass.Tester.SessionID == tester.SessionID || pass.Tester.AuthorityID == tester.AuthorityID || pass.Tester.RuntimeSessionID == tester.RuntimeSessionID {
				return record, errors.New("tester session replay denied")
			}
		}
		if err = e.revalidate(ctx, c, p, candidate); err != nil {
			return record, err
		}
		if check.Outcome == "passed" {
			if err = e.checkpoint(ctx, "review", record, candidate.Digest); err != nil {
				return record, err
			}
			check.Review, err = e.persistReview(candidate, p)
			if err != nil {
				return record, err
			}
		}
		pass := ContributionPass{candidate, tester, check}
		data, _ := json.Marshal(pass)
		if err = e.DB.Create([]byte(fmt.Sprintf("contribution/acceptance/%s/%d", digest([]byte(run)), record.Consumed)), data); err != nil {
			return record, err
		}
		record.Passes = append(record.Passes, pass)
		record.Frozen = nil
		if check.Outcome == "passed" {
			record.State = "candidate_accepted"
			return record, e.save(record)
		}
		previous, _ = json.Marshal(check.Criteria)
		record.Expected = cloneContext(expected)
		record.BoundaryIndex = candidate.Index
		record.BoundarySource = frozen.Digest
		record.State = "phase_boundary"
		if err = e.save(record); err != nil {
			return record, err
		}
	}
	record.State = "candidate_budget_exhausted"
	return record, e.save(record)
}
func validContributionRole(r ContributionRoleIdentity) bool {
	return r.AgentDigest != "" && r.CharterDigest != "" && r.AuthorityID != "" && r.AuthorityDigest != "" && r.SessionID != "" && r.MandateID != "" && r.RuntimeSessionID != "" && r.TurnID != ""
}
func stdlibModule(s SourceInventory) error {
	for _, f := range s.Files {
		if f.Path == "go.mod" {
			m, err := modfile.Parse("go.mod", f.Content, nil)
			if err != nil || m.Module == nil || len(m.Require) > 0 || len(m.Replace) > 0 || len(m.Exclude) > 0 || len(m.Tool) > 0 {
				return errors.New("only dependency-free Go modules are qualified")
			}
			return nil
		}
	}
	return errors.New("Go module required")
}
func onlyEdits(before, after SourceInventory, edits []Edit) error {
	expected := map[string]string{}
	for _, f := range before.Files {
		expected[f.Path] = digest(f.Content)
	}
	for _, edit := range edits {
		expected[edit.Path] = digest(edit.Content)
	}
	if len(expected) != len(after.Files) {
		return errors.New("unexpected source inventory mutation")
	}
	for _, f := range after.Files {
		if expected[f.Path] != digest(f.Content) {
			return errors.New("unexpected source inventory mutation")
		}
	}
	return nil
}
func (e *ManagedContribution) git(ctx context.Context, p loop.ContributionExecutionPolicy, dir string, args ...string) ([]byte, error) {
	if err := e.admit(ctx, "git-read-or-worktree"); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, p.GitBinary, append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Env = []string{"PATH=" + filepath.Dir(p.GitBinary), "HOME=" + e.StateRoot, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	return e.commandOutput(ctx, cmd)
}
func (e *ManagedContribution) prepare(ctx context.Context, p loop.ContributionExecutionPolicy, workspace, run, base string) error {
	origin, err := e.git(ctx, p, p.RepositoryPath, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	// Git transport is limited to public canonical HTTPS; no ambient SSH/helper/PAT.
	if e.Repository == "" || strings.TrimSpace(string(origin)) != "https://github.com/"+e.Repository+".git" {
		return errors.New("exact approved public HTTPS origin required")
	}
	if e.Fetch != nil {
		err = e.Fetch(ctx, p)
	} else {
		_, err = e.git(ctx, p, p.RepositoryPath, "fetch", "--no-tags", "origin", p.BaseBranch)
	}
	if err != nil {
		return err
	}
	fetched, err := e.git(ctx, p, p.RepositoryPath, "rev-parse", "FETCH_HEAD")
	if err != nil || strings.TrimSpace(string(fetched)) != base {
		return errors.New("approved base changed")
	}
	_, err = e.git(ctx, p, p.RepositoryPath, "worktree", "add", "-b", "contribution-"+strings.TrimPrefix(digest([]byte(run)), "sha256:"), workspace, base)
	return err
}
func (e *ManagedContribution) native(ctx context.Context, c loop.VerifiedImplementation, source SourceInventory, args []string) ([]byte, error) {
	toolchain, toolErr := e.verifyToolchain(ctx)
	if toolErr != nil {
		return nil, toolErr
	}
	if err := e.admit(ctx, "native-check"); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.Policy.TimeoutSeconds)*time.Second)
	defer cancel()
	home, err := os.MkdirTemp(e.StateRoot, "check-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(home)
	workspace := filepath.Join(home, "source")
	if err = source.Materialize(workspace); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, e.GoBinary, args...)
	cmd.Dir = workspace
	cmd.Env = []string{"PATH=" + filepath.Dir(e.GoBinary), "HOME=" + home, "GOCACHE=" + filepath.Join(home, "cache"), "GOMODCACHE=" + filepath.Join(home, "modules"), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0", "GOMAXPROCS=2", "GOMEMLIMIT=2GiB", "GOFLAGS=-p=1 -trimpath -buildvcs=false"}
	cmd.Env = append(cmd.Env, "GOROOT="+toolchain.Root, "GOENV=off", "GOOS="+toolchain.OS, "GOARCH="+toolchain.Arch, "GOAMD64=v1", "GOEXPERIMENT=", "GODEBUG=", "TMPDIR="+home)
	if e.CurrentExecutable != "" {
		b, err := e.DB.Get(blobKey(e.CurrentExecutable))
		if err != nil || digest(b) != e.CurrentExecutable {
			return nil, errors.New("candidate executable custody mismatch")
		}
		executable := filepath.Join(home, "candidate")
		if err = os.WriteFile(executable, b, 0500); err != nil {
			return nil, err
		}
		cmd.Env = append(cmd.Env, "AEGIS_CONTRIBUTION_CANDIDATE="+executable)
	}
	if e.Native == nil {
		if err = configureProcess(cmd); err != nil {
			return nil, err
		}
		defer killProcessGroup(cmd)
	}
	cmd.WaitDelay = time.Second
	var output boundedOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	if e.Native != nil {
		err = e.Native(ctx, cmd)
	} else {
		err = cmd.Run()
	}
	if _, toolErr := e.verifyToolchain(ctx); toolErr != nil {
		return nil, toolErr
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if output.overflow {
		return nil, errors.New("native check output exceeds bound")
	}
	return output.bytes, err
}
func (e *ManagedContribution) check(ctx context.Context, c loop.VerifiedImplementation, source SourceInventory) (ContributionCheck, []byte, error) {
	args := append([]string{"test", "-json", "-count=1", "-p=1", fmt.Sprintf("-timeout=%ds", c.Policy.TimeoutSeconds)}, c.Policy.Packages...)
	output, err := e.native(ctx, c, source, args)
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return ContributionCheck{}, nil, err
	}
	status := "failed"
	if err == nil && requiredTestsPassed(output, c.Policy.RequiredTests) {
		status = "passed"
	}
	d, err := e.blob(output)
	return ContributionCheck{Outcome: status, OutputDigest: d, RequiredTests: c.Policy.RequiredTests}, output, err
}
func (e *ManagedContribution) build(ctx context.Context, c loop.VerifiedImplementation, p loop.ContributionExecutionPolicy, source SourceInventory) (string, error) {
	// native performs complete toolchain validation before and after execution.
	if err := e.admit(ctx, "build"); err != nil {
		return "", err
	}
	// Output is outside source; its content-addressed bytes stay in controller custody.
	f, err := os.CreateTemp(e.StateRoot, "candidate-")
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	if _, err = e.native(ctx, c, source, []string{"build", "-mod=readonly", "-o", path, p.BuildPackage}); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 || len(b) > 32<<20 {
		return "", errors.New("bounded executable artifact required")
	}
	return e.blob(b)
}
func (e *ManagedContribution) revalidate(ctx context.Context, c loop.VerifiedImplementation, p loop.ContributionExecutionPolicy, v ContributionCandidate) error {
	if err := e.admit(ctx, "candidate-verify"); err != nil {
		return err
	}
	source, err := Inventory(ctx, c.Workspace)
	if err != nil || source.Digest != v.Source.Digest {
		return errors.New("frozen source changed")
	}
	for _, x := range []struct {
		args     []string
		expected string
		hashed   bool
	}{{[]string{"rev-parse", "HEAD"}, v.HeadCommit, false}, {[]string{"rev-parse", "HEAD^{tree}"}, v.HeadTree, false}, {[]string{"ls-files", "--stage", "-z"}, v.Index, true}} {
		b, err := e.git(ctx, p, c.Workspace, x.args...)
		if err != nil {
			return err
		}
		actual := strings.TrimSpace(string(b))
		if x.hashed {
			actual = digest(b)
		}
		if actual != x.expected {
			return errors.New("frozen Git identity changed")
		}
	}
	b, err := e.DB.Get(blobKey(v.Executable))
	if err != nil || digest(b) != v.Executable {
		return errors.New("candidate executable missing or substituted")
	}
	// Source, Git and blob observations execute no Go code. Inventory the entire
	// toolchain once at this final validation point, still against the frozen ID.
	if _, err = e.verifyToolchain(ctx); err != nil || e.toolchainDigest != v.Toolchain {
		return errors.New("frozen toolchain changed")
	}
	return nil
}

func independentContributionRole(b, t ContributionRoleIdentity) bool {
	return validContributionRole(t) && t.AgentDigest != b.AgentDigest && t.CharterDigest != b.CharterDigest && t.AuthorityID != b.AuthorityID && t.SessionID != b.SessionID && t.MandateID != b.MandateID && t.RuntimeSessionID != b.RuntimeSessionID
}
