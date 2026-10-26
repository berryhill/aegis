package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/store"
)

type ContributionResult struct {
	Disposition disposition.Record                  `json:"disposition"`
	Artifact    evidence.RuntimeArtifact            `json:"artifact"`
	Receipts    []evidence.VerificationReceipt      `json:"receipts"`
	Report      evidence.ContributionTerminalReport `json:"report"`
}

// Retrieval uses retained owner/key and canonical terminal indexes only. No
// runtime, source custody, old lease or delivery/reconciliation call is involved.
func (s *Service) GetContributionResultAs(ctx context.Context, sub core.Subject, key string) (ContributionResult, error) {
	var out ContributionResult
	c, _, _, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return out, err
	}
	d, err := s.FleetRepository.GetDispositionByGraphRun(ctx, c.RunID)
	if err != nil {
		return out, err
	}
	if d.GraphRunID != c.RunID || d.QueueItem.ID != c.QueueItemID || len(d.ArtifactIDs) != 1 || len(d.ReceiptIDs) != 1 {
		return out, ErrDenied
	}
	item, err := s.FleetRepository.GetQueueItem(ctx, c.QueueItemID)
	if err != nil || d.QueueItem.Digest != item.Digest || item.GraphRunID != c.RunID {
		return out, ErrDenied
	}
	if _, err = disposition.Marshal(d); err != nil {
		return out, ErrDenied
	}
	attempt, err := s.FleetRepository.GetAttempt(ctx, d.AttemptID)
	if err != nil || attempt.QueueItem != d.QueueItem || attempt.GraphRunID != c.RunID || attempt.LoopExecutionID != d.LoopExecutionID {
		return out, ErrDenied
	}
	claim, err := s.FleetRepository.GetClaim(ctx, attempt.ClaimID)
	if err != nil || claim.AttemptID != attempt.AttemptID || claim.QueueItem != d.QueueItem {
		return out, ErrDenied
	}
	executed, err := s.FleetRepository.GetLoopExecution(ctx, d.LoopExecutionID)
	if err != nil || executed.Loop != c.Loop || executed.Participant != c.Controller || executed.GraphRunID != c.RunID {
		return out, ErrDenied
	}
	a, err := s.FleetRepository.GetRuntimeArtifact(ctx, d.ArtifactIDs[0])
	if err != nil {
		return out, err
	}
	if a.Validate() != nil || a.ID != d.ArtifactIDs[0] || a.AttemptID != d.AttemptID || a.RunID != d.LoopExecutionID || a.ActionID != d.LoopExecutionID || a.OwnerID != c.Controller.ID || a.AuthorityContextID != d.Authority.ID || a.AuthorityContextDigest != d.Authority.Digest || a.MediaType != evidence.ContributionFinalMediaType {
		return out, ErrDenied
	}
	b, err := s.Store.GetBlob(a.ContentRef)
	if err != nil {
		return out, err
	}
	r, err := evidence.DecodeContributionTerminalReport(a.ContentRef, b)
	if err != nil || r.Owner != sub.PrincipalID || r.Key != key || r.Contract != c.Digest || r.Run != c.RunID || r.Attempt != d.AttemptID || r.Reason != d.ReasonCode {
		return out, ErrDenied
	}
	terminalState, terminalReason, supported := evidence.ContributionTerminalMapping(r.Outcome)
	if r.Claim != claim.ClaimID || !supported || d.State != execution.State(terminalState) || d.ReasonCode != terminalReason || !contribution.DeliveryNarrows(c.Effect, r.Scope) {
		return out, ErrDenied
	}
	if r.Current == nil {
		if d.Authority != claim.Authority {
			return out, ErrDenied
		}
	} else if r.Current.OriginalAuthority != claim.Authority || r.Current.Authority != d.Authority {
		return out, ErrDenied
	}
	projection, err := s.FleetRepository.GetQueueProjection(ctx, c.QueueItemID)
	if err != nil || projection.State != queue.State(terminalState) {
		return out, ErrDenied
	}
	receipt, err := s.FleetRepository.GetVerificationReceipt(ctx, d.ReceiptIDs[0])
	if err != nil {
		return out, err
	}
	wire, err := s.Store.GetBlob(receipt.EvidenceRef)
	if err != nil {
		return out, err
	}
	loaded, err := evidence.DecodeVerificationReceipt(receipt.EvidenceRef, wire)
	if err != nil || loaded != receipt || !contributionResultReceiptMatches(d, a, receipt) {
		return out, ErrDenied
	}
	return ContributionResult{d, a, []evidence.VerificationReceipt{receipt}, r}, nil
}

// ContributionCompletionAdapter is a trusted controller-owned conductor adapter.
// Only actual VerifyPreClosureAs can populate its opaque authorization.
type ContributionCompletionAdapter struct {
	verifier      *ContributionFinalVerifier
	subject       core.Subject
	key           string
	mu            sync.Mutex
	authorization *ContributionPreClosureAuthorization
	reference     string
}

func NewContributionCompletionAdapter(v *ContributionFinalVerifier, sub core.Subject, key string) (*ContributionCompletionAdapter, error) {
	if v == nil || !contribution.ValidKey(key) {
		return nil, ErrDenied
	}
	return &ContributionCompletionAdapter{verifier: v, subject: sub, key: key}, nil
}
func (a *ContributionCompletionAdapter) VerifyContributionCompletion(ctx context.Context, ref delivery.Reference) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	proof, _, err := a.verifier.VerifyPreClosureAs(ctx, a.subject, a.key)
	if err != nil {
		return "", err
	}
	if proof.contract != ref.Contract || proof.candidate != ref.Candidate {
		return "", ErrDenied
	}
	wire, _ := json.Marshal(struct{ Schema, Owner, Key, Contract, Candidate, Native, History, Continuity, Policy, Claim, Attempt string }{"aegis.contribution.preclosure.v1", proof.owner, proof.key, proof.contract, proof.candidate, proof.native, proof.history, proof.continuity, proof.policy, proof.claim, proof.attempt})
	stored, err := a.verifier.service.Store.PutBlob(wire)
	if err != nil {
		return "", err
	}
	a.authorization, a.reference = proof, stored
	return stored, nil
}

// FinalizeContributionCompletion is invoked only AFTER canonical issue readback.
func (a *ContributionCompletionAdapter) FinalizeContributionCompletion(ctx context.Context, ref delivery.Reference) error {
	return a.FinalizeContributionCompletionAs(ctx, a.subject, ref)
}

// A fresh same-owner subject is observation only until the real current role,
// compatible mandate, continuation and private source gates admit finalization.
func (a *ContributionCompletionAdapter) FinalizeContributionCompletionAs(ctx context.Context, subject core.Subject, ref delivery.Reference) error {
	a.mu.Lock()
	authorization, preclosure := a.authorization, a.reference
	a.mu.Unlock()
	// Snapshot immutable verifier-owned handles only. Never hold a local mutex
	// across custody/winner waiting: caller cancellation must also work when
	// concurrent calls happen to share this adapter instance.
	if authorization == nil || preclosure == "" {
		return ErrDenied
	}
	_, err := a.verifier.finalizeContributionAs(ctx, subject, a.key, authorization, preclosure, ref)
	return err
}

type contributionTerminalOwner interface {
	CompleteContributionQueueItem(context.Context, fleet.Completion, fleet.AuditFact, fleet.EvidenceReader) error
}

func (v *ContributionFinalVerifier) finalizeContributionAs(ctx context.Context, sub core.Subject, key string, auth *ContributionPreClosureAuthorization, preclosure string, ref delivery.Reference) (ContributionResult, error) {
	var out ContributionResult
	if auth == nil || auth.service != v.service || auth.owner != sub.PrincipalID || auth.key != key || auth.contract != ref.Contract || auth.candidate != ref.Candidate {
		return out, ErrDenied
	}
	s := v.service
	// Idempotency is a read of the exact canonical winner, never effect replay.
	if existing, e := s.GetContributionResultAs(ctx, sub, key); e == nil {
		if !contributionWinnerMatches(existing, auth, preclosure, ref) {
			return out, ErrDenied
		}
		return existing, nil
	} else if !errors.Is(e, fleet.ErrNotFound) {
		return out, e
	}
	c, state, events, err := s.ObserveContributionContinuityAs(ctx, sub, key)
	if err != nil {
		return out, err
	}
	role := state.RoleAuthorities["controller"]
	action := func(cctx context.Context, candidate orchestration.ContributionCandidate) error {
		c, current, historyEvents, e := s.ObserveContributionContinuityAs(cctx, sub, key)
		if e != nil || verifyFinalContinuity(c, current, historyEvents) != nil || !compatiblePreclosureContinuity(c, current, historyEvents, auth.continuity) || current.RoleAuthorities["controller"] != role {
			return ErrDenied
		}
		definition, e := s.FleetRepository.GetLoopRevision(cctx, c.Loop.ID, c.Loop.Revision)
		if e != nil || definition.Contribution == nil || definition.Contribution.FinalVerification != evidence.ContributionFinalPolicy {
			return ErrDenied
		}
		control, e := s.ReadContributionControlAs(cctx, sub, key)
		if e != nil || control.Stop {
			return ErrDenied
		}
		scope, e := effectiveContributionScope(c, control)
		if e != nil || scope != auth.scope {
			return ErrDenied
		}
		request := contribution.EffectAdmissionRequest{Contract: referenceForContribution(c), RunID: c.RunID, QueueItemID: c.QueueItemID, Candidate: candidate.Digest, CandidateEvidence: current.CandidateEvidence, Role: "controller", Epoch: role.Epoch, RoleAuthority: role.Ref(), Scope: c.Effect}
		if _, e = s.CheckContributionEffectAs(cctx, sub, role.SessionID, key, request, true); e != nil {
			return e
		}
		native, e := s.ReadContributionCandidateEvidenceAs(cctx, sub, key)
		if e != nil || native.EvidenceRef != auth.native {
			return ErrDenied
		}
		record, e := s.ReadContributionExecutionAs(cctx, sub, key)
		if e != nil || s.verifyFinalNativeRoles(c, current, historyEvents, record) != nil {
			return ErrDenied
		}
		h, e := s.ReadContributionDeliveryHistoryAs(cctx, sub, key)
		if e != nil {
			return e
		}
		prior := h
		prior.Histories = nil
		issue := ""
		fullSuccess := auth.outcome == "succeeded"
		for _, chain := range h.Histories {
			last := chain[len(chain)-1]
			if !fullSuccess || last.Record.Intent.Operation != delivery.ReconcileIssue {
				prior.Histories = append(prior.Histories, chain)
				continue
			}
			if issue != "" || last.Record.Outcome != delivery.Confirmed || last.Record.Intent.Reference != ref {
				return ErrDenied
			}
			proof, e := delivery.ParseReadback(last.Record.Intent, last.Record.Observation)
			if e != nil || proof.Requirements == nil || proof.Requirements.Completion != preclosure || proof.Requirements.IssueID != c.Source.IssueID || proof.Requirements.IssueNumber != uint64(c.Source.URL.Number) {
				return ErrDenied
			}
			issue = last.Digest
		}
		wire, _ := json.Marshal(prior)
		if contribution.Digest(wire) != auth.history || (fullSuccess && issue == "") {
			return ErrDenied
		}
		p, e := v.policies.ResolveContributionDeliveryPolicy(cctx, v.policy)
		if e != nil || p.Digest() != auth.policy {
			return ErrDenied
		}
		source := &ContributionDeliverySource{service: s, subject: sub, key: key, policyRef: v.policy, policies: v.policies}
		plan, e := source.project(cctx, p.Binding.Reference, p, candidate)
		if e != nil {
			return e
		}
		plan.Completion = preclosure
		if fullSuccess {
			pr, err := retainedContributionPR(h, p)
			if err != nil {
				return err
			}
			plan.PR = pr
		}
		for _, chain := range h.Histories {
			last := chain[len(chain)-1]
			if fullSuccess && last.Record.Intent.Operation == delivery.ReconcileIssue && delivery.ValidateIntentForPlan(plan, last.Record.Intent) != nil {
				return ErrDenied
			}
		}
		_, view, e := s.ReadContributionAs(cctx, sub, key)
		if e != nil {
			return e
		}
		claim, e := s.FleetRepository.GetClaim(cctx, view.Projection.ActiveClaimID)
		if e != nil || claim.ClaimID != auth.claim || claim.AttemptID != auth.attempt {
			return ErrDenied
		}
		currentCompletion, e := s.admitContributionCompleter(cctx, sub, c, current, claim)
		if e != nil {
			return e
		}
		completingAuthority := claim.Authority
		if currentCompletion != nil {
			completingAuthority = currentCompletion.Authority
		}
		attempt, e := s.FleetRepository.GetAttempt(cctx, claim.AttemptID)
		if e != nil {
			return e
		}
		controlWire, _ := json.Marshal(control)
		fullWire, _ := json.Marshal(h)
		historyRef, e := s.Store.PutBlob(fullWire)
		if e != nil {
			return e
		}
		terminalState, terminalReason, supported := evidence.ContributionTerminalMapping(auth.outcome)
		if !supported {
			return ErrDenied
		}
		report := evidence.ContributionTerminalReport{Schema: evidence.ContributionFinalPolicy, Owner: c.OwnerID, Key: c.Key, Run: c.RunID, Contract: c.Digest, Scope: scope, Outcome: auth.outcome, Reason: terminalReason, Cause: auth.cause, Candidate: candidate.Digest, Policy: p.Digest(), Native: auth.native, History: historyRef, Continuity: current.Digest, Control: contribution.Digest(controlWire), PreClosure: preclosure, Issue: issue, MergeRefs: []string{}, Claim: claim.ClaimID, Attempt: claim.AttemptID}
		for _, chain := range h.Histories {
			for _, entry := range chain {
				if entry.Record.Merge != "" {
					report.MergeRefs = append(report.MergeRefs, entry.Digest)
				}
			}
		}
		report.Current = currentCompletion
		reportWire, e := evidence.MarshalContributionTerminalReport(report)
		if e != nil {
			return e
		}
		reportRef, e := s.Store.PutBlob(reportWire)
		if e != nil {
			return e
		}
		now := s.Now().UTC()
		artifact := evidence.RuntimeArtifact{ID: store.ID("artifact"), AttemptID: claim.AttemptID, OwnerID: c.Controller.ID, ActionID: attempt.LoopExecutionID, RunID: attempt.LoopExecutionID, AuthorityContextID: completingAuthority.ID, AuthorityContextDigest: completingAuthority.Digest, Digest: reportRef, ContentRef: reportRef, MediaType: evidence.ContributionFinalMediaType, CreatedAt: now}
		receipt := evidence.VerificationReceipt{ID: store.ID("verification"), AttemptID: artifact.AttemptID, ArtifactID: artifact.ID, ActionID: artifact.ActionID, RunID: artifact.RunID, OwnerID: artifact.OwnerID, AuthorityContextID: artifact.AuthorityContextID, AuthorityContextDigest: artifact.AuthorityContextDigest, VerifierID: "aegis-contribution-verifier", PolicyVersion: evidence.ContributionFinalPolicy, Claim: evidence.ContributionFinalClaim, MediaType: artifact.MediaType, ExpectedDigest: reportRef, ObservedDigest: reportRef, Outcome: evidence.Passed, ObservedAt: now}
		receiptWire, _ := json.Marshal(receipt)
		receipt.EvidenceRef, e = s.Store.PutBlob(receiptWire)
		if e != nil {
			return e
		}
		verifier, e := evidence.NewBlobVerifier(s.Store)
		if e != nil {
			return e
		}
		provenance, e := verifier.SealContributionReportReceipt(cctx, artifact, receipt)
		if e != nil {
			return e
		}
		d, e := disposition.New(disposition.Record{DispositionID: store.ID("disposition"), GraphRunID: c.RunID, LoopExecutionID: attempt.LoopExecutionID, AttemptID: claim.AttemptID, QueueItem: claim.QueueItem, Authority: completingAuthority, State: execution.State(terminalState), ReasonCode: report.Reason, ArtifactIDs: []string{artifact.ID}, ReceiptIDs: []string{receipt.ID}, OccurredAt: now})
		if e != nil {
			return e
		}
		transition, e := queue.NewTransition(queue.QueueTransition{TransitionID: store.ID("transition"), QueueItemID: c.QueueItemID, ClaimID: claim.ClaimID, From: queue.StateClaimed, To: queue.State(terminalState), Reason: report.Reason, OccurredAt: now})
		if e != nil {
			return e
		}
		original, e := s.FleetRepository.GetQueueRuntimeBinding(cctx, c.QueueItemID)
		if e != nil {
			return e
		}
		currentAuthority, e := s.authorityContextForSession(cctx, role.SessionID)
		if e != nil || currentAuthority.ID != role.Authority.ID || currentAuthority.Digest != role.Authority.Digest {
			return ErrDenied
		}
		fact := fleet.AuditFact{Event: core.AuditEvent{Type: "fleet.disposition.recorded", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, AgentID: c.Controller.ID, StanzaID: currentAuthority.Authority.StanzaID, MandateID: original.MandateID, Outcome: string(execution.StateSucceeded), Reason: report.Reason, Metadata: map[string]string{"disposition_id": d.DispositionID, "transition_id": transition.TransitionID, "terminal_at": now.Format(time.RFC3339Nano), "completing_authority": role.Authority.ID, "original_authority": claim.Authority.ID}}}
		owner, ok := s.FleetRepository.(contributionTerminalOwner)
		fact.Event.Outcome = terminalState
		if currentCompletion != nil {
			fact.Event.MandateID = currentCompletion.Mandate
			fact.Event.Metadata["original_mandate"] = currentCompletion.OriginalMandate
			fact.Event.Metadata["completing_mandate"] = currentCompletion.Mandate
		}
		if !ok {
			return ErrDenied
		}
		if _, e = s.CheckContributionEffectAs(cctx, sub, role.SessionID, key, request, true); e != nil {
			return e
		}
		proof, ok := cctx.Value(contributionCustodyKey{}).(*contributionCustodyProof)
		if !ok || proof == nil || !proof.live.Load() || proof.service != s || proof.terminal == nil {
			return ErrDenied
		}
		completion := fleet.Completion{Claim: claim, Artifact: &artifact, Receipts: []evidence.VerificationReceipt{receipt}, Disposition: d, Transition: transition, Provenance: provenance}
		return proof.terminal.register(proof, &contributionPreparedTerminal{contract: c.Digest, candidate: candidate.Digest, session: role.SessionID, commit: func(commitCtx context.Context) error {
			// General effect authority has ended. Repeat ordinary current role and
			// immutable head admission for this metadata-only commit; do not use a
			// retained private effect context or run any native/GitHub operation.
			if commitCtx.Err() != nil {
				return ErrDenied
			}
			freshCompletion, admissionErr := s.admitContributionCompleter(commitCtx, sub, c, current, claim)
			oldBinding, _ := json.Marshal(currentCompletion)
			freshBinding, _ := json.Marshal(freshCompletion)
			if admissionErr != nil || string(oldBinding) != string(freshBinding) {
				return ErrDenied
			}
			checked, e := s.CheckContributionRoleAs(commitCtx, sub, role.SessionID, key, "controller", ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: current.Sequence, ExpectedDigest: current.Digest})
			if e != nil || checked != role {
				return ErrDenied
			}
			latestPolicy, e := v.policies.ResolveContributionDeliveryPolicy(commitCtx, v.policy)
			if e != nil || latestPolicy.Digest() != p.Digest() {
				return ErrDenied
			}
			latestControl, e := s.ReadContributionControlAs(commitCtx, sub, key)
			if e != nil || latestControl != control {
				return ErrDenied
			}
			latestHistory, e := s.ReadContributionDeliveryHistoryAs(commitCtx, sub, key)
			latestWire, _ := json.Marshal(latestHistory)
			if e != nil || contribution.Digest(latestWire) != historyRef {
				return ErrDenied
			}
			commitAt := s.Now().UTC()
			completion.Disposition.OccurredAt = commitAt
			completion.Disposition, e = disposition.New(completion.Disposition)
			if e != nil {
				return e
			}
			completion.Transition.OccurredAt = commitAt
			completion.Transition, e = queue.NewTransition(completion.Transition)
			if e != nil {
				return e
			}
			fact.Event.Metadata["terminal_at"] = commitAt.Format(time.RFC3339Nano)
			return owner.CompleteContributionQueueItem(commitCtx, completion, fact, s.Store)
		}})
	}
	_ = events
	_ = c
	if proof, ok := ctx.Value(contributionCustodyKey{}).(*contributionCustodyProof); ok {
		if proof == nil || !proof.live.Load() || proof.service != s || proof.session != role.SessionID || proof.contract != auth.contract || proof.candidate != auth.candidate {
			return out, ErrDenied
		}
		candidate, e := s.AcceptedContributionCandidateAs(ctx, sub, key)
		if e != nil {
			return out, e
		}
		err = action(ctx, candidate)
		if err == nil && proof.terminal.registered() {
			// The owning Source's terminal-last frame will commit synchronously
			// after every existing postcheck. No result is claimed here yet.
			return out, nil
		}
	} else {
		err = s.WithContributionContinuityCandidateAs(ctx, sub, role.SessionID, key, action)
	}
	if err != nil {
		if errors.Is(err, orchestration.ErrContributionCustodyBusy) {
			return v.reconcileContributionWinner(ctx, sub, key, auth, preclosure, ref)
		}
		return out, err
	}
	return s.GetContributionResultAs(ctx, sub, key)
}
func referenceForContribution(c contribution.Contract) reference.DigestRef {
	return reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: c.QueueItemID, Digest: c.Digest}
}
