package delivery

import (
	"context"
	"errors"
	"sync"
)

// DeliveryResult is a transient projection of canonical effects, not disposition.
// A completed delivery stage never claims whole-contribution success.
type DeliveryResult struct {
	Reference
	Completed             []Operation
	Held                  Operation
	Reason                string
	Effects               []Record
	Head, Merge, BaseHead string
	PR                    uint64
	Completion            string
}

// CompletionOwner belongs to #379. Implementations reload the full trusted scope
// and return its immutable verified reference, not a caller receipt or PASS flag.
type CompletionOwner interface {
	VerifyContributionCompletion(context.Context, Reference) (string, error)
}

// FinalCompletionOwner is called only after canonical issue reconciliation.
// Preclosure verification alone never terminalizes the contribution.
type FinalCompletionOwner interface {
	FinalizeContributionCompletion(context.Context, Reference) error
}

type BoundaryCompletionOwner interface {
	FinalizeContributionBoundary(context.Context, Reference) error
}

type Conductor struct {
	engine     *Engine
	repair     RepairOwner
	completion CompletionOwner
	readiness  ReadinessOption
}

func NewConductor(e *Engine, repair RepairOwner, completion CompletionOwner, readiness ...ReadinessOption) *Conductor {
	c := &Conductor{engine: e, repair: repair, completion: completion}
	if len(readiness) == 1 {
		c.readiness = readiness[0]
	}
	return c
}

// Run is bounded and synchronous. It holds ONE accepted-source callback through
// every effect and repair handoff. It installs no worker, watcher, or polling loop.
func (c *Conductor) Run(caller context.Context, ref Reference) (out DeliveryResult, err error) {
	out.Reference = ref
	if c == nil || c.engine == nil {
		return out, ErrDenied
	}
	e := c.engine
	if e.owner == nil || e.source == nil || e.authority == nil || e.adapter == nil {
		return out, ErrDenied
	}
	var mu sync.Mutex
	active, called := true, false
	err = e.source.WithAccepted(caller, ref, func(ctx context.Context, p Plan) error {
		mu.Lock()
		defer mu.Unlock()
		if !active || called || ctx == nil || ctx.Err() != nil || caller.Err() != nil || p.Reference != ref {
			return ErrDenied
		}
		called = true
		// Ignore all caller/policy PR and completion receipts. Only our own canonical
		// PR result and the trusted full-scope owner can supply these references.
		p.PR, p.Completion = 0, ""
		finishBoundary := func() error {
			if owner, ok := c.completion.(BoundaryCompletionOwner); ok {
				return owner.FinalizeContributionBoundary(ctx, ref)
			}
			return nil
		}
		used := uint8(0)
		run := func(op Operation) (bool, error) {
			out.Held = op
			var r Record
			var x error
			for {
				if op == CheckDelivery || op == Merge {
					r, x = e.executePlanReadiness(ctx, p, op, caller, func(previous Record) error {
						return c.readiness.reserve(ctx, caller, p, previous.Intent, &used)
					})
				} else {
					r, x = e.executeAccepted(ctx, p, op, caller)
				}
				if x != nil || r.Outcome != Unknown || !(op == CheckDelivery || op == Merge && r.Merge != "") {
					break
				}
				// Retain each observed merge fact even if a later reservation holds.
				out.Effects = append(out.Effects, r)
				if r.Merge != "" {
					out.Merge = r.Merge
				}
				if r.BaseHead != "" {
					out.BaseHead = r.BaseHead
				}
				if c.readiness.budget == nil || c.readiness.policy.maximum == 0 {
					break
				}
			}
			if x != nil {
				if r.Intent.ID != "" {
					out.Effects = append(out.Effects, r)
					if r.Merge != "" {
						out.Merge = r.Merge
					}
					if r.BaseHead != "" {
						out.BaseHead = r.BaseHead
					}
					if r.PR != 0 {
						out.PR = r.PR
					}
					if r.Head != "" {
						out.Head = r.Head
					}
				}
				out.Reason = "effect_unavailable"
				if errors.Is(x, errReadinessHeld) {
					out.Reason = x.Error()
					return false, nil
				}
				return false, x
			}
			out.Effects = append(out.Effects, r)
			if r.Head != "" {
				out.Head = r.Head
			}
			if r.PR != 0 {
				out.PR = r.PR
			}
			if r.Merge != "" {
				out.Merge = r.Merge
			}
			if r.BaseHead != "" {
				out.BaseHead = r.BaseHead
			}
			if r.Outcome != Confirmed {
				out.Reason = r.Code
				if out.Merge != "" {
					out.Reason = "merged_unresolved"
				}
				if r.Outcome == Failed && (r.Code == "ci_failed" || r.Code == "merge_conflict") {
					out.Reason = "repair_owner_unavailable"
					if c.repair != nil {
						if ForwardRepair(ctx, c.repair, r) == nil {
							out.Reason = "repair_requested"
						} else {
							out.Reason = "repair_held"
						}
					}
				}
				return false, nil
			}
			out.Completed = append(out.Completed, op)
			out.Held = ""
			return true, nil
		}
		if ok, x := run(Commit); !ok || x != nil {
			if x == nil {
				return finishBoundary()
			}
			return x
		}
		if p.Delivery == "local-only" || p.Delivery == "no-publish" {
			out.Reason = "local_target_completed"
			return finishBoundary()
		}
		if ok, x := run(Publish); !ok || x != nil {
			if x == nil {
				return finishBoundary()
			}
			return x
		}
		if ok, x := run(PullRequest); !ok || x != nil {
			if x == nil {
				return finishBoundary()
			}
			return x
		}
		if out.PR == 0 {
			out.Held, out.Reason = PullRequest, "canonical_pr_missing"
			return nil
		}
		p.PR = out.PR
		if ok, x := run(CheckDelivery); !ok || x != nil {
			if x == nil {
				return finishBoundary()
			}
			return x
		}
		if p.Delivery == "no-merge" {
			out.Reason = "published_checked_target_completed"
			return finishBoundary()
		}
		if ok, x := run(Merge); !ok || x != nil {
			if x == nil {
				return finishBoundary()
			}
			return x
		}
		out.Held, out.Reason = ReconcileIssue, "completion_owner_unavailable_issue_open"
		if c.completion == nil {
			return nil
		}
		completion, x := c.completion.VerifyContributionCompletion(ctx, ref)
		if x != nil || !roleDigest.MatchString(completion) {
			out.Reason = "completion_unverified_issue_open"
			return nil
		}
		p.Completion, out.Completion = completion, completion
		if ok, x := run(ReconcileIssue); !ok || x != nil {
			if x == nil {
				return finishBoundary()
			}
			return x
		}
		if finalizer, ok := c.completion.(FinalCompletionOwner); ok {
			if x := finalizer.FinalizeContributionCompletion(ctx, ref); x != nil {
				out.Reason = "final_disposition_unavailable"
				return x
			}
		}
		out.Reason = "verified_completion_issue_reconciled"
		return nil
	})
	mu.Lock()
	active = false
	if !called && err == nil {
		err = ErrDenied
	}
	mu.Unlock()
	return
}
