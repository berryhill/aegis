package app

import (
	"context"
	"encoding/json"

	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
)

// This is read-only diagnostic projection bracketed by native owner reloads. It
// grants no live source custody or permission to execute the returned plan.
func projectRetainedPublication(ctx context.Context, git *delivery.Git, candidate orchestration.ContributionCandidate, p ContributionDeliveryPolicy) (delivery.CommitSpec, []delivery.GitObject, error) {
	parent := p.BaseHead
	if p.RepairFailure != "" {
		parent = p.ExpectedOld
	}
	spec, objects, err := git.ProjectAccepted(ctx, candidate, delivery.CommitSpec{Parent: parent, Tree: p.Tree, Branch: p.Branch, Author: p.Author, Committer: p.Committer, Timestamp: p.Timestamp, Message: p.Message}, p.AllowedFiles)
	if err != nil {
		return spec, nil, delivery.ErrDenied
	}
	observed := git.Reconcile(ctx, spec)
	if observed.Outcome != delivery.Confirmed || observed.Head != spec.Commit || observed.Tree != spec.Tree {
		return spec, nil, delivery.ErrDenied
	}
	return spec, objects, nil
}

func assessRetainedPublication(h delivery.HistorySnapshot, p ContributionDeliveryPolicy, target string, spec delivery.CommitSpec, objects []delivery.GitObject) string {
	byID := map[string]delivery.GitObject{}
	for _, o := range objects {
		byID[o.OID] = o
	}
	stamp, err := time.Parse(time.RFC3339, p.Timestamp)
	if err != nil {
		return "publication_invalid"
	}

	for _, chain := range h.Histories {
		r := chain[len(chain)-1].Record
		i := r.Intent
		if i.Candidate != p.Binding.Candidate {
			continue
		}
		if r.Outcome != delivery.Confirmed {
			switch r.Outcome {
			case delivery.Unknown:
				return "delivery_unknown"
			case delivery.Denied:
				return "delivery_denied"
			default:
				return "delivery_failed"
			}
		}
		if i.Operation == delivery.Commit {
			if i.Head != spec.Commit || r.Head != spec.Commit || r.Tree != spec.Tree {
				return "publication_invalid"
			}
			continue
		}
		proof, err := delivery.ParseReadback(i, r.Observation)
		if err != nil {
			return "proof_invalid"
		}
		if i.Operation == delivery.ReconcileIssue {
			return "completion_authority_unavailable"
		}
		if i.Operation == delivery.UploadBlob || i.Operation == delivery.UploadTree || i.Operation == delivery.UploadCommit {
			o, ok := byID[r.Head]
			if !ok || i.Head != spec.Commit || r.Head != o.OID {
				return "object_invalid"
			}
			objectWire, _ := json.Marshal(o)
			if proof.Requirements == nil || proof.Requirements.ObjectDigest != contribution.Digest(objectWire) {
				return "object_invalid"
			}
			want := map[string]delivery.Operation{"blob": delivery.UploadBlob, "tree": delivery.UploadTree, "commit": delivery.UploadCommit}[o.Kind]
			if i.Operation != want {
				return "object_invalid"
			}
			if i.Operation == delivery.UploadCommit {
				req := proof.Requirements
				if req == nil || req.Publication == nil || req.Policy != p.Digest() {
					return "publication_invalid"
				}
				pub := req.Publication
				if i.Head != spec.Commit || pub.Tree != spec.Tree || pub.Parent != spec.Parent || pub.MessageDigest != contribution.Digest([]byte(p.Message)) || pub.Author.Name != p.Author.Name || pub.Author.Email != p.Author.Email || pub.Committer.Name != p.Committer.Name || pub.Committer.Email != p.Committer.Email {
					return "publication_authorship_invalid"
				}
				// Adapter dates may use equivalent UTC offset spelling; compare instants.
				for _, v := range []string{pub.Author.Date, pub.Committer.Date} {
					parsed, e := time.Parse(time.RFC3339, v)
					if e != nil || !parsed.Equal(stamp) {
						return "publication_timestamp_invalid"
					}
				}
			}
			continue
		}
		if i.Head != spec.Commit {
			return "head_invalid"
		}
		switch i.Operation {
		case delivery.CheckDelivery, delivery.Merge:
			if !retainedDeliveryRequirements(proof.Requirements, p, target) {
				return "requirements_invalid"
			}
		case delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest:
			pr := r.PR
			if i.Operation == delivery.UpdatePullRequest {
				// An optional effect cannot select the target it is being checked
				// against. Resolve from the owned creation or independent policy pin.
				var e error
				pr, e = retainedContributionPR(h, p)
				if e != nil || pr == 0 || i.PR != pr || r.PR != pr {
					return "pr_invalid"
				}
			} else if i.Operation == delivery.PullRequest && p.PR != 0 && pr != p.PR {
				return "pr_invalid"
			}
			if !retainedDeliveryPublication(proof, p, pr) {
				return "publication_readback_invalid"
			}
		case delivery.TransferGraph:
			// Exact graph payload is checked separately by the owning canonical
			// intent builder, against the independently projected object graph.
		default:
			return "operation_invalid"
		}
	}

	return "publication_assessed"
}

// Resolve the existing owned creation, never an optional update's own target.
// A reviewed nonzero pin is only a constraint; it cannot substitute for an
// independently matched canonical creation effect.
func retainedContributionPR(h delivery.HistorySnapshot, p ContributionDeliveryPolicy) (uint64, error) {
	pr, found := p.PR, false
	for _, chain := range h.Histories {
		if len(chain) == 0 {
			return 0, delivery.ErrDenied
		}
		r := chain[len(chain)-1].Record
		if (r.Intent.Reference != p.Binding.Reference && !retainedRepairPRAncestor(h, p, r)) || r.Intent.Operation != delivery.PullRequest || r.Outcome != delivery.Confirmed {
			continue
		}
		if found || r.PR == 0 || (pr != 0 && r.PR != pr) {
			return 0, delivery.ErrDenied
		}
		pr, found = r.PR, true
	}
	if pr == 0 || !found {
		return 0, delivery.ErrDenied
	}
	return pr, nil
}
