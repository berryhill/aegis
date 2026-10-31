// Package delivery contains closed controller-owned delivery effects. It is not
// a Queue, credential broker or completion verifier. No production owner wiring
// is installed merely by importing this package.
package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
)

type Operation string

const (
	Commit            Operation = "git.scoped_commit.v1"
	Publish           Operation = "github.feature_ref.v1"
	PullRequest       Operation = "github.non_draft_pr.v1"
	UpdatePullRequest Operation = "github.update_owned_pr.v1"
	CheckDelivery     Operation = "github.check_delivery.v1"
	Merge             Operation = "github.merge.v1"
	ReconcileIssue    Operation = "github.issue_disposition.v1"
)

type Outcome string

const (
	Unknown   Outcome = "unknown"
	Denied    Outcome = "denied"
	Failed    Outcome = "failed"
	Confirmed Outcome = "confirmed"
)

var (
	ErrDenied   = errors.New("delivery denied")
	ErrConflict = errors.New("delivery reservation conflict")
)

type Reference struct{ Run, Contract, Candidate string }

// Plan is an operational projection supplied ONLY by Source.WithAccepted. It is
// NOT a second frozen candidate/acceptance format. Candidate is the real #376
// candidate digest; CommitSpec is materialized from that accepted Git object.
// Source must reload the sealed intake, actual independent acceptance chain,
// current control and candidate bytes; it must hold quiescent source custody
// throughout fn. There is deliberately no caller-controlled passed flag.
type Plan struct {
	FenceEpoch  uint64
	RoleBinding string
	Reference
	Intake                                                      contribution.Contract
	Actor, Repository                                           string
	RepositoryID                                                uint64
	Base, Branch, Head, BaseHead, ExpectedOld, Policy, Delivery string
	PR                                                          uint64
	PRTitle, PRBody                                             string
	ProtectionPolicy                                            string
	RequiredChecks, RequiredReviewers, PostMergeChecks          []string
	MergeMethod                                                 string
	Commit                                                      CommitSpec
	Objects                                                     []GitObject
	Object                                                      GitObject
	// Completion is an immutable reference to #379's full-scope verification,
	// never a boolean or a model-authored report. Source must reload and verify it
	// before returning an issue-close plan. Empty means retain issue open.
	Completion string
}
type Intent struct {
	FenceEpoch  uint64
	RoleBinding string
	ID          string
	Reference
	Operation                                                  Operation
	Actor, Repository                                          string
	RepositoryID                                               uint64
	Base, Branch, Head, BaseHead, ExpectedOld, Policy, Payload string
	PR                                                         uint64
}
type Observation struct {
	ProofJSON, ProofDigest            string
	Outcome                           Outcome
	Code, Head, Tree, Merge, BaseHead string
	PR                                uint64
}
type Record struct {
	Intent  Intent
	Outcome Outcome
	Observation
	Revision uint64
}

// Repository must be implemented in the EXISTING canonical persistence owner.
// Reserve atomically holds stable repository+base publication custody for Run
// across ALL operations/candidates/phases, reserves exact intent before effects,
// and returns fresh=true to at most one claimant. Existing/unknown intents are
// read-only reconciliation, NEVER permission to resend. Crash after reservation
// can permanently remain unknown until operation-specific proof is available.
// Finish uses revision CAS, validates immutable intent, preserves prior facts.
// Fence checks current fenced epoch/lease, stop acknowledgement and controls;
// writing=false may allow authorized reconciliation after cancellation. No lease
// expiry alone releases ownership; no replacement claimant while effect unknown.
type Repository interface {
	Reserve(context.Context, Intent) (Record, bool, error)
	Finish(context.Context, Record, Observation) (Record, error)
	Fence(context.Context, Intent, bool) error
}
type Source interface {
	WithAccepted(context.Context, Reference, func(context.Context, Plan) error) error
}

// Authority must perform fresh role/mandate/scope/control admission for this
// exact intent at each guard invocation. #377 implementation remains required.
type Authority interface {
	Admit(context.Context, Intent, bool) error
}
type Guard func(context.Context) error
type Adapter interface {
	Apply(context.Context, Intent, Plan, Guard) Observation
	Reconcile(context.Context, Intent, Plan, Guard) Observation
}
type Engine struct {
	owner     Repository
	source    Source
	authority Authority
	adapter   Adapter
}

func NewEngine(r Repository, s Source, a Authority, d Adapter) *Engine { return &Engine{r, s, a, d} }

var roleDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

var sha = regexp.MustCompile(`^[0-9a-f]{40}$`)
var repoName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}/[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var branchName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_/-]{0,199}$`)

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:])
}
func intentFor(p Plan, op Operation) (Intent, error) {
	// Apply to direct uploads AND every subsequent operation, not just Publish.
	// Reject before Reserve or adapter admission so no partial publication can
	// consume the finite reconstructable history envelope.
	if len(p.Objects) > maxPublicationObjects && p.Delivery != "local-only" && p.Delivery != "no-publish" {
		return Intent{}, ErrDenied
	}
	if p.FenceEpoch == 0 || !roleDigest.MatchString(p.RoleBinding) || p.Run == "" || p.Contract == "" || p.Candidate == "" || p.Actor == "" || p.RepositoryID == 0 || !repoName.MatchString(p.Repository) || !branchName.MatchString(p.Base) || !branchName.MatchString(p.Branch) || p.Base == p.Branch || !sha.MatchString(p.Head) || !sha.MatchString(p.BaseHead) || p.Policy == "" {
		return Intent{}, ErrDenied
	}
	switch op {
	case Commit, Publish, PullRequest, UpdatePullRequest, CheckDelivery, Merge, ReconcileIssue, TransferGraph, UploadBlob, UploadTree, UploadCommit:
	default:
		return Intent{}, ErrDenied
	}
	// The effect slot is stable across candidate/payload repairs. Changing an
	// immutable payload conflicts with that slot, including after UNKNOWN; it
	// never manufactures a second write permission. Owners retain the old intent.
	i := Intent{FenceEpoch: p.FenceEpoch, RoleBinding: p.RoleBinding, Reference: p.Reference, Operation: op, Actor: p.Actor, Repository: p.Repository, RepositoryID: p.RepositoryID, Base: p.Base, Branch: p.Branch, Head: p.Head, BaseHead: p.BaseHead, ExpectedOld: p.ExpectedOld, Policy: p.Policy}
	i.ID = digest(struct {
		Run, Contract, Repository, Base, Branch string
		RepositoryID                            uint64
		Operation                               Operation
	}{p.Run, p.Contract, p.Repository, p.Base, p.Branch, p.RepositoryID, op})
	if op == TransferGraph {
		if validateGraph(p) != nil {
			return Intent{}, ErrDenied
		}
		i.Payload = digest(struct {
			Intent  Intent
			Objects []GitObject
		}{i, p.Objects})
		return i, nil
	}
	if op == UploadBlob || op == UploadTree || op == UploadCommit {
		if !objectValid(p.Object) || objectOp(p.Object) != op {
			return Intent{}, ErrDenied
		}
		found := false
		for _, o := range p.Objects {
			if o.OID == p.Object.OID && o.Kind == p.Object.Kind && digest(o) == digest(p.Object) {
				found = true
			}
		}
		if !found || validateGraph(p) != nil {
			return Intent{}, ErrDenied
		}
		i.ID = digest(struct{ Slot, OID string }{i.ID, p.Object.OID})
		i.Payload = digest(struct {
			Intent       Intent
			ObjectDigest string
			Publication  *ReadbackPublication
		}{i, digest(p.Object), publicationFor(p.Object)})
		return i, nil
	}
	// Positive operation schemas, not whole-Plan hashing with exclusions.
	type common struct {
		FenceEpoch  uint64
		RoleBinding string
		Reference
		Actor, Repository, Base, Branch, Head, BaseHead, ExpectedOld, Policy, Delivery, Worktree string
		RepositoryID                                                                             uint64
	}
	c := common{p.FenceEpoch, p.RoleBinding, p.Reference, p.Actor, p.Repository, p.Base, p.Branch, p.Head, p.BaseHead, p.ExpectedOld, p.Policy, p.Delivery, p.Intake.Worktree, p.RepositoryID}
	switch op {
	case Commit:
		i.Payload = digest(struct {
			Target common
			Commit CommitSpec
		}{c, p.Commit})
	case Publish:
		i.Payload = digest(struct {
			Target common
			Commit CommitSpec
		}{c, p.Commit})
	case PullRequest:
		i.Payload = digest(struct {
			Target      common
			Title, Body string
		}{c, p.PRTitle, p.PRBody})
	case UpdatePullRequest:
		i.PR = p.PR
		i.Payload = digest(struct {
			Target      common
			PR          uint64
			Title, Body string
		}{c, p.PR, p.PRTitle, p.PRBody})
	case CheckDelivery, Merge:
		i.PR = p.PR
		i.Payload = digest(struct {
			Target                        common
			PR                            uint64
			Method, Tree, Protection      string
			Checks, Reviewers, PostChecks []string
		}{c, p.PR, p.MergeMethod, p.Commit.Tree, p.ProtectionPolicy, p.RequiredChecks, p.RequiredReviewers, p.PostMergeChecks})
	case ReconcileIssue:
		i.PR = p.PR
		i.Payload = digest(struct {
			Target          common
			Completion      string
			IssueID, Number uint64
		}{c, p.Completion, p.Intake.Source.IssueID, uint64(p.Intake.Source.URL.Number)})
	}
	return i, nil
}
func permits(target string, op Operation) bool {
	switch target {
	case "local-only", "no-publish":
		return op == Commit
	case "no-merge":
		return op == Commit || op == Publish || op == PullRequest || op == UpdatePullRequest || op == CheckDelivery || op == UploadBlob || op == UploadTree || op == UploadCommit || op == TransferGraph
	case "verified-protected-base-merge":
		return true
	}
	return false
}
func (e *Engine) Execute(ctx context.Context, ref Reference, op Operation) (result Record, err error) {
	if e == nil || e.owner == nil || e.source == nil || e.authority == nil || e.adapter == nil {
		return result, ErrDenied
	}
	var mu sync.Mutex
	active, called := true, false
	caller := ctx
	err = e.source.WithAccepted(ctx, ref, func(ctx context.Context, p Plan) error {
		mu.Lock()
		defer mu.Unlock()
		if !active || called || ctx == nil || ctx.Err() != nil || caller.Err() != nil {
			return ErrDenied
		}
		called = true
		if p.Reference != ref {
			return ErrDenied
		}
		result, err = e.executeAccepted(ctx, p, op, caller)
		return err
	})
	mu.Lock()
	active = false
	if !called && err == nil {
		err = ErrDenied
	}
	mu.Unlock()
	return
}

// executeAccepted runs an operation without reacquiring source custody.
func (e *Engine) executeAccepted(ctx context.Context, p Plan, op Operation, caller context.Context) (result Record, err error) {
	if op == UploadBlob || op == UploadTree || op == UploadCommit {
		root, x := e.executePlan(ctx, p, TransferGraph, caller)
		if x != nil {
			return result, x
		}
		if root.Outcome != Confirmed {
			result = root
			return result, nil
		}
	}
	if op == Publish && len(p.Objects) > 0 {
		if validateGraph(p) != nil {
			return result, ErrDenied
		}
		// Reserve the immutable whole-graph projection before any child. Repairs
		// cannot switch graph/OIDs to escape an UNKNOWN child effect.
		root, x := e.executePlan(ctx, p, TransferGraph, caller)
		if x != nil {
			return result, x
		}
		if root.Outcome != Confirmed {
			result = root
			return result, nil
		}
		for _, o := range p.Objects {
			child := p
			child.Object = o
			r, x := e.executePlan(ctx, child, objectOp(o), caller)
			if x != nil {
				return result, x
			}
			if r.Outcome != Confirmed {
				result = r
				return result, nil
			}
		}
	}
	return e.executePlan(ctx, p, op, caller)
}

func (e *Engine) executePlan(ctx context.Context, p Plan, op Operation, caller context.Context) (result Record, err error) {
	return e.executePlanReadiness(ctx, p, op, caller, nil)
}

// beforeRead runs after canonical reservation and before any extra readiness HTTP.
func (e *Engine) executePlanReadiness(ctx context.Context, p Plan, op Operation, caller context.Context, beforeRead func(Record) error) (result Record, err error) {
	var live atomic.Bool
	live.Store(true)
	defer live.Store(false)
	err = func() error {
		if ctx == nil || ctx.Err() != nil || caller.Err() != nil {
			return ErrDenied
		}
		i, x := intentFor(p, op)
		if x != nil {
			return x
		}
		r, fresh, x := e.owner.Reserve(ctx, i)
		if x != nil {
			return x
		}
		if r.Intent != i {
			return ErrConflict
		}
		result = r
		if r.Outcome == Confirmed || r.Outcome == Denied || r.Outcome == Failed {
			return nil
		}
		// Decide freshness BEFORE reading. A first Check reservation is read-only,
		// but remains fresh for persistence/CAS; it is not a second reservation.
		if !fresh && (op == CheckDelivery || op == Merge && r.Merge != "") {
			if beforeRead == nil {
				return nil
			}
			if x := beforeRead(r); x != nil {
				return x
			}
		}
		guard := func(c context.Context) error {
			if !live.Load() || caller.Err() != nil || ctx.Err() != nil || c == nil || c.Err() != nil {
				return ErrDenied
			}
			if !fresh && (op == CheckDelivery || op == Merge && r.Merge != "") && (p.Intake.Deadline.IsZero() || !time.Now().Before(p.Intake.Deadline)) {
				return ErrDenied
			}
			if e.owner.Fence(ctx, i, fresh && op != CheckDelivery) != nil || e.authority.Admit(ctx, i, fresh && op != CheckDelivery) != nil {
				return ErrDenied
			}
			return nil
		}
		var observation Observation
		if guard(ctx) != nil {
			if !fresh {
				return ErrDenied
			}
			observation = Observation{Outcome: Denied, Code: "admission_denied"}
		} else if fresh && !permits(p.Delivery, op) {
			observation = Observation{Outcome: Denied, Code: "delivery_restricted"}
		} else if fresh && op != CheckDelivery {
			observation = e.adapter.Apply(ctx, i, p, guard)
		} else {
			observation = e.adapter.Reconcile(ctx, i, p, guard)
		}
		switch observation.Outcome {
		case Unknown, Denied, Failed, Confirmed:
		default:
			observation = Observation{Outcome: Unknown, Code: "adapter_outcome_invalid"}
		}
		// A duplicate may observe absence while the original write is still in
		// flight. Only positive facts may advance its record; a negative read
		// cannot terminalize or consume the original writer's CAS revision.
		positive := observation.Head != "" || observation.Tree != "" || observation.Merge != "" || observation.PR != 0
		if !fresh && observation.Outcome != Confirmed && !positive {
			return nil
		}
		if x = validateObservation(i, p, observation); x != nil {
			return x
		}
		// One bounded CAS recovery: reload only the SAME reserved intent. Never
		// resend an effect or allocate a replacement slot after a competing Finish.
		for attempt := 0; attempt < 2; attempt++ {
			merged, mergeErr := retainObservation(r, observation)
			if mergeErr != nil {
				return mergeErr
			}
			if !fresh && op != CheckDelivery && merged.Outcome != Confirmed {
				merged.Outcome = Unknown
			}
			if ctx.Err() != nil || caller.Err() != nil {
				return ErrDenied
			}
			result, x = e.owner.Finish(ctx, r, merged)
			if !errors.Is(x, ErrConflict) || attempt == 1 {
				return x
			}
			var allocated bool
			r, allocated, x = e.owner.Reserve(ctx, i)
			if x != nil {
				return x
			}
			if allocated || r.Intent != i {
				return ErrConflict
			}
			result = r
			if r.Outcome != Unknown {
				return nil // Canonical terminal facts win; no stale overwrite.
			}
			fresh = false
		}
		return ErrConflict
	}()
	return
}

// RepairFailure is handed to the existing #376/#377 cumulative budget owner.
// This package never starts a builder, increments a second budget or resets one.
type RepairFailure struct {
	Reference
	Intent, Class, Criterion, Evidence, Head, Base string
}
type RepairOwner interface {
	RequestRepair(context.Context, RepairFailure) error
}
