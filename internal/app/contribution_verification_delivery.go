package app

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
)

// RetainedDeliveryAssessment is diagnostic retained evidence, never a completion
// authorization, current remote admission, source lease or terminal disposition.
// It deliberately exposes no source bytes, native proof or remote raw records.
type RetainedDeliveryAssessment struct {
	Status     string   `json:"status"`
	Code       string   `json:"code"`
	NativeRef  string   `json:"native_ref,omitempty"`
	HistoryRef string   `json:"history_ref,omitempty"`
	MergeRefs  []string `json:"merge_refs"`
}

// AssessContributionRetainedDeliveryAs joins actual #376 and #378 owners. The
// resolver is installed by the trusted controller, not supplied by a model. It
// performs no GitHub/runtime effects and never enters a source-custody callback.
func (s *Service) AssessContributionRetainedDeliveryAs(ctx context.Context, subject core.Subject, key string, ref reference.DigestRef, policies ContributionDeliveryPolicyResolver) (RetainedDeliveryAssessment, error) {
	out := RetainedDeliveryAssessment{Status: "not-verified", Code: "evidence_invalid", MergeRefs: []string{}}
	if s == nil || policies == nil || ref.Validate() != nil || ctx.Err() != nil {
		return out, ErrDenied
	}
	c, _, err := s.ReadContributionAs(ctx, subject, key)
	if err != nil {
		return out, ErrDenied
	}
	h, err := s.ReadContributionDeliveryHistoryAs(ctx, subject, key)
	if err != nil {
		return out, ErrDenied
	}
	// Retain irreversible history even when later checks, native readback or policy
	// fail. Only independently valid same-root sealed envelopes supply these refs.
	for _, chain := range h.Histories {
		for _, e := range chain {
			if delivery.ValidateHistoryEnvelope(e) == nil && e.Record.Intent.Run == c.RunID && e.Record.Intent.Contract == c.Digest && e.Record.Merge != "" {
				if !slices.Contains(out.MergeRefs, e.Digest) {
					out.MergeRefs = append(out.MergeRefs, e.Digest)
				}
			}
		}
	}
	fail := func(code string) (RetainedDeliveryAssessment, error) { out.Code = code; return out, ErrDenied }
	p, err := policies.ResolveContributionDeliveryPolicy(ctx, ref)
	p = p.snapshot()
	if err != nil || p.Digest() != ref.Digest {
		return fail("policy_invalid")
	}
	control, err := s.ReadContributionControlAs(ctx, subject, key)
	if err != nil {
		return fail("control_invalid")
	}
	target := c.Effect
	if control.Sequence > 0 {
		target, err = contribution.RestrictDelivery(target, control.Delivery)
		if err != nil {
			return fail("control_invalid")
		}
	}
	if delivery.ValidateHistorySnapshot(h) != nil || p.Binding.OwnerID != subject.PrincipalID || p.Binding.Key != key || p.Binding.QueueItemID != c.QueueItemID || p.Binding.Run != c.RunID || p.Binding.Contract != c.Digest || p.Loop != c.Loop || h.Contract.Digest != c.Digest || h.Root.Base != p.Binding.Base || h.Root.Branch != p.Branch || p.Workspace != c.Worktree || p.BaseHead != c.Base || p.Binding.FenceEpoch == 0 || p.Binding.RoleBinding == "" {
		return fail("binding_invalid")
	}
	n, err := s.ReadContributionCandidateEvidenceAs(ctx, subject, key)
	if err != nil {
		return fail("native_unavailable")
	}
	out.NativeRef = n.EvidenceRef
	wire, err := s.Store.GetBlob(n.EvidenceRef)
	// The actual native owner already validated canonical producer bytes. Project
	// only existing fields here, retaining the orchestration-owned candidate alias
	// rather than importing the protected implementation layer or copying its DTO.
	var native map[string]json.RawMessage
	if err != nil || contribution.Digest(wire) != n.EvidenceRef || json.Unmarshal(wire, &native) != nil {
		return fail("native_invalid")
	}
	text := func(key string) string { var value string; _ = json.Unmarshal(native[key], &value); return value }
	var passes []map[string]json.RawMessage
	if json.Unmarshal(native["passes"], &passes) != nil || text("schema") != "aegis.contribution.native-acceptance.v1" || text("run_id") != c.RunID || text("contract") != c.Digest || text("queue_attempt") != p.QueueAttempt || n.Attempt != p.QueueAttempt || n.Candidate != p.Binding.Candidate || len(passes) == 0 {
		return fail("native_invalid")
	}
	var candidate orchestration.ContributionCandidate
	if json.Unmarshal(passes[len(passes)-1]["candidate"], &candidate) != nil {
		return fail("native_invalid")
	}
	revision, err := s.FleetRepository.GetLoopRevision(ctx, c.Loop.ID, c.Loop.Revision)
	if err != nil || revision.Digest != c.Loop.Digest || revision.Contribution == nil || revision.Contribution.Managed == nil {
		return fail("definition_invalid")
	}
	policyWire := native["policy"]
	expectedPolicy, _ := json.Marshal(revision.Contribution.Managed)
	if !bytes.Equal(policyWire, expectedPolicy) || candidate.Digest != n.Candidate || candidate.Source.Digest != p.Source || candidate.CandidateTree != p.Tree || candidate.Toolchain != p.Toolchain || candidate.Policy != p.CandidatePolicy || candidate.Base != c.Base || candidate.Builder.AgentDigest != c.Builder.Digest || len(c.Roles) < 2 || candidate.Builder.CharterDigest != c.Roles[1].Charter.Digest || revision.Contribution.Managed.BaseBranch != p.Binding.Base {
		return fail("candidate_invalid")
	}
	code := assessRetainedDeliveryHistory(h, p, target)
	// Reuse the actual owning Source's pure projection. This does not enter its
	// effect/custody runner and cannot grant source or GitHub authority.
	projection := &ContributionDeliverySource{service: s, subject: subject, key: key, policyRef: ref, policies: policies}
	plan, projectionErr := projection.project(ctx, p.Binding.Reference, p, candidate)
	if projectionErr != nil {
		return fail("publication_projection_invalid")
	}
	git := &delivery.Git{Binary: revision.Contribution.Managed.GitBinary, Directory: c.Worktree}
	actualCommit := git.Reconcile(ctx, plan.Commit)
	if actualCommit.Outcome != delivery.Confirmed || actualCommit.Head != plan.Head || actualCommit.Tree != plan.Commit.Tree {
		return fail("publication_local_state_invalid")
	}
	if code == "retained_delivery_verified" {
		code = assessRetainedPublication(h, p, target, plan.Commit, plan.Objects)
	}
	if code == "publication_assessed" {
		code = assessRetainedIntentPayloads(h, p, plan)
	}
	// Store reference-only history identity in the existing private blob owner;
	// this does not create a mutable aggregate or grant authority.
	historyWire, _ := json.Marshal(h)
	out.HistoryRef, err = s.Store.PutBlob(historyWire)
	if err != nil {
		return fail("history_invalid")
	}
	loaded, err := s.Store.GetBlob(out.HistoryRef)
	if err != nil || !bytes.Equal(loaded, historyWire) || contribution.Digest(loaded) != out.HistoryRef {
		return fail("history_invalid")
	}
	current, err := policies.ResolveContributionDeliveryPolicy(ctx, ref)
	if err != nil || current.Digest() != p.Digest() {
		return fail("policy_changed")
	}
	after, err := s.ReadContributionDeliveryHistoryAs(ctx, subject, key)
	afterWire, _ := json.Marshal(after)
	if err != nil || !bytes.Equal(historyWire, afterWire) {
		return fail("history_changed")
	}
	afterControl, err := s.ReadContributionControlAs(ctx, subject, key)
	if err != nil || afterControl != control {
		return fail("control_changed")
	}
	again, err := s.ReadContributionCandidateEvidenceAs(ctx, subject, key)
	if err != nil || again != n || ctx.Err() != nil {
		return fail("native_changed")
	}
	if control.Stop {
		return fail("run_stopped")
	}
	out.Code = code
	if code == "intent_payloads_assessed" {
		out.Status = "retained-delivery-evidence-assessed"
		out.Code = "continuity_verification_required"
	}
	return out, nil
}

func retainedDeliveryRequired(target string) []delivery.Operation {
	switch target {
	case "local-only", "no-publish":
		return []delivery.Operation{delivery.Commit}
	case "no-merge":
		return []delivery.Operation{delivery.Commit, delivery.Publish, delivery.PullRequest, delivery.CheckDelivery}
	case "verified-protected-base-merge":
		return []delivery.Operation{delivery.Commit, delivery.Publish, delivery.PullRequest, delivery.CheckDelivery, delivery.Merge}
	}
	return nil
}
func retainedDeliveryRequirements(r *delivery.ReadbackRequirements, p ContributionDeliveryPolicy, target string) bool {
	return r != nil && r.Policy == p.Digest() && r.Delivery == target && r.Worktree == p.Workspace && r.Tree == p.Tree && r.Method == p.MergeMethod && r.Protection == p.ProtectionPolicy && slices.Equal(r.Checks, p.RequiredChecks) && slices.Equal(r.Reviewers, p.RequiredReviewers) && slices.Equal(r.PostChecks, p.PostMergeChecks)
}

func assessRetainedDeliveryHistory(h delivery.HistorySnapshot, p ContributionDeliveryPolicy, target string) string {
	required := retainedDeliveryRequired(target)
	if required == nil || delivery.ValidateHistorySnapshot(h) != nil {
		return "history_invalid"
	}
	current := map[delivery.Operation]delivery.Record{}
	head := ""
	var pr uint64
	for _, chain := range h.Histories {
		r := chain[len(chain)-1].Record
		i := r.Intent
		switch i.Operation {
		case delivery.Commit, delivery.Publish, delivery.PullRequest, delivery.UpdatePullRequest, delivery.CheckDelivery, delivery.Merge, delivery.ReconcileIssue, delivery.TransferGraph, delivery.UploadBlob, delivery.UploadTree, delivery.UploadCommit:
		default:
			return "operation_invalid"
		}
		if (target == "local-only" || target == "no-publish") && i.Operation != delivery.Commit {
			return "scope_invalid"
		}
		if target == "no-merge" && (i.Operation == delivery.Merge || i.Operation == delivery.ReconcileIssue) {
			return "scope_invalid"
		}
		for _, envelope := range chain {
			if envelope.Record.ProofJSON != "" || envelope.Record.ProofDigest != "" {
				if _, err := delivery.ParseReadback(i, envelope.Record.Observation); err != nil {
					return "proof_invalid"
				}
			}
		}
		if i.Candidate != p.Binding.Candidate {
			continue
		} // earlier immutable failures remain history, never current success.
		if i.Reference != p.Binding.Reference || i.FenceEpoch != p.Binding.FenceEpoch || i.RoleBinding != p.Binding.RoleBinding || i.Actor != p.Actor || i.Repository != h.Contract.Repository || i.RepositoryID != h.Contract.Source.RepositoryID || i.Base != p.Binding.Base || i.Branch != p.Branch || i.BaseHead != p.BaseHead || i.ExpectedOld != p.ExpectedOld || i.Policy != p.Digest() {
			return "binding_invalid"
		}
		if i.Operation == delivery.UploadBlob || i.Operation == delivery.UploadTree || i.Operation == delivery.UploadCommit {
			if r.Outcome != delivery.Confirmed {
				return "delivery_partial"
			}
			continue
		}
		if _, ok := current[i.Operation]; ok {
			return "duplicate_effect"
		}
		current[i.Operation] = r
		if i.Operation == delivery.Commit {
			head = r.Head
			if r.Tree != p.Tree || head != i.Head || head == "" {
				return "tree_invalid"
			}
		}
		if i.Operation == delivery.PullRequest {
			pr = r.PR
			if pr == 0 {
				return "pr_invalid"
			}
		}
	}
	for _, op := range required {
		r, ok := current[op]
		if !ok {
			return "delivery_partial"
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
		if r.Intent.Head != head || (r.Head != "" && r.Head != head) {
			return "head_invalid"
		}
		if op != delivery.Commit {
			proof, err := delivery.ParseReadback(r.Intent, r.Observation)
			if err != nil {
				return "proof_invalid"
			}
			if (op == delivery.Publish || op == delivery.PullRequest) && !retainedDeliveryPublication(proof, p, pr) {
				return "publication_invalid"
			}
			if op == delivery.CheckDelivery || op == delivery.Merge {
				if r.Intent.PR != pr || !retainedDeliveryRequirements(proof.Requirements, p, target) {
					return "requirements_invalid"
				}
			}
			if op == delivery.PullRequest && r.PR != pr {
				return "pr_invalid"
			}
		}
	}
	if p.PR != 0 && p.PR != pr {
		return "pr_invalid"
	}
	return "retained_delivery_verified"
}
