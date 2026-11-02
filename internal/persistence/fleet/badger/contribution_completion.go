package badger

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	badgerdb "github.com/dgraph-io/badger/v4"
	"slices"
)

func exactContributionTerminalEvidence(txn *badgerdb.Txn, attempt execution.Attempt, completion fleet.Completion, r evidence.ContributionTerminalReport) bool {
	c, e := loadContribution(txn, attempt.QueueItem.ID)
	if e != nil || r.Contract != c.Digest || r.Owner != c.OwnerID || r.Key != c.Key || r.Run != c.RunID || r.Claim != completion.Claim.ClaimID || r.Attempt != attempt.AttemptID || r.Attempt != completion.Claim.AttemptID || r.Reason != completion.Disposition.ReasonCode {
		return false
	}
	revisionWire, e := get(txn, key(familyLoopRevision, c.Loop.ID, revisionPart(c.Loop.Revision)))
	if e != nil {
		return false
	}
	revision, e := loop.UnmarshalRevision(revisionWire)
	if e != nil || revision.Digest != c.Loop.Digest || revision.Contribution == nil || revision.Contribution.FinalVerification != evidence.ContributionFinalPolicy {
		return false
	}
	state, _, e := loadContributionContinuity(txn, c.QueueItemID)
	if e != nil || state.Digest != r.Continuity || state.CancelRequested || state.CandidateEvidence.Digest != r.Candidate || state.Used.QueueAttempts != 1 || state.Used.Candidates > uint32(c.CandidateBudget) {
		return false
	}
	nativeOwner := implementation.ManagedContribution{DB: contributionCompletionSnapshot{txn}}
	native, e := nativeOwner.Read(c.RunID)
	if e != nil || native.State != "candidate_accepted" || native.Contract != c.Digest || native.QueueAttempt != r.Attempt || len(native.Passes) == 0 || native.Passes[len(native.Passes)-1].Candidate.Digest != r.Candidate || uint32(native.Consumed) != state.Used.Candidates {
		return false
	}
	export := implementation.ContributionAcceptanceEvidence{Integration: native.Integration, DeliveryRepairs: native.DeliveryRepairs, Schema: implementation.ContributionAcceptanceSchema, RunID: c.RunID, Contract: c.Digest, QueueAttempt: native.QueueAttempt, Consumed: native.Consumed, Policy: native.Policy, Baseline: native.Baseline, Passes: native.Passes}
	exported, _ := json.Marshal(export)
	if contribution.Digest(exported) != r.Native {
		return false
	}
	control, e := loadContributionControl(txn, c.QueueItemID)
	if e != nil || control.Stop {
		return false
	}
	scope := c.Effect
	if control.Sequence > 0 {
		scope, e = contribution.RestrictDelivery(c.Effect, control.Delivery)
		if e != nil {
			return false
		}
	}
	if r.Scope != scope {
		return false
	}
	controlWire, _ := json.Marshal(control)
	if contribution.Digest(controlWire) != r.Control {
		return false
	}
	role, ok := state.RoleAuthorities["controller"]
	expectedAuthority := completion.Claim.Authority
	if r.Current != nil {
		expectedAuthority = r.Current.Authority
	}
	if !ok || role.Authority != expectedAuthority {
		return false
	}
	for _, name := range []string{"builder", "tester"} {
		if !contribution.NativeRoleQuiescent(state, name) {
			return false
		}
	}
	if !completion.Disposition.OccurredAt.Before(c.Deadline) {
		return false
	}
	rootWire, e := get(txn, key(familyContributionPublication, "root", c.OwnerID, c.Key))
	if e != nil {
		return false
	}
	var root delivery.PublicationRoot
	if strictDelivery(rootWire, &root) != nil {
		return false
	}
	contractWire, e := get(txn, key(familyContribution, c.QueueItemID))
	if e != nil {
		return false
	}
	h := delivery.HistorySnapshot{Contract: c, ContractBytes: contractWire, RootRun: c.RunID, Root: root, RootBytes: rootWire}
	h.Histories, e = publicationHistories(txn, root)
	if e != nil || !publicationRootValid(root) {
		return false
	}
	if delivery.ValidateHistorySnapshot(h) != nil {
		return false
	}
	merges := []string{}
	for _, chain := range h.Histories {
		for _, entry := range chain {
			if entry.Record.Merge != "" {
				merges = append(merges, entry.Digest)
			}
		}
	}
	if !slices.Equal(merges, r.MergeRefs) {
		return false
	}
	wire, _ := json.Marshal(h)
	if contribution.Digest(wire) != r.History {
		return false
	}
	if r.Outcome == "succeeded" {
		if r.PreClosure == "" || r.Issue == "" {
			return false
		}
		found := false
		for _, chain := range h.Histories {
			last := chain[len(chain)-1]
			if last.Digest != r.Issue || last.Record.Intent.Candidate != r.Candidate || last.Record.Intent.Policy != r.Policy || last.Record.Intent.Operation != delivery.ReconcileIssue || last.Record.Outcome != delivery.Confirmed {
				continue
			}
			proof, e := delivery.ParseReadback(last.Record.Intent, last.Record.Observation)
			if e == nil && proof.Requirements != nil && proof.Requirements.Completion == r.PreClosure && proof.Requirements.IssueID == c.Source.IssueID && proof.Requirements.IssueNumber == uint64(c.Source.URL.Number) {
				found = true
			}
		}
		if !found {
			return false
		}
	} else if r.Outcome == "narrower-target-completed" {
		if r.Scope == "verified-protected-base-merge" {
			return false
		}
		required := map[delivery.Operation]bool{delivery.Commit: false}
		if r.Scope == "no-merge" {
			required[delivery.Publish] = false
			required[delivery.PullRequest] = false
			required[delivery.CheckDelivery] = false
		}
		head := ""
		for _, chain := range h.Histories {
			last := chain[len(chain)-1].Record
			if last.Intent.Candidate == r.Candidate && last.Intent.Operation == delivery.Commit {
				if head != "" || last.Outcome != delivery.Confirmed || last.Tree != native.Passes[len(native.Passes)-1].Candidate.CandidateTree || last.Head != last.Intent.Head {
					return false
				}
				head = last.Head
			}
		}
		if head == "" {
			return false
		}
		for _, chain := range h.Histories {
			op := chain[0].Record.Intent.Operation
			if op == delivery.Merge || op == delivery.ReconcileIssue || ((r.Scope == "local-only" || r.Scope == "no-publish") && op != delivery.Commit) {
				return false
			}
			last := chain[len(chain)-1].Record
			if _, needed := required[op]; needed && last.Intent.Candidate == r.Candidate {
				if required[op] || last.Outcome != delivery.Confirmed || last.Intent.Policy != r.Policy || last.Intent.Head != head {
					return false
				}
				if op != delivery.Commit {
					if _, e := delivery.ParseReadback(last.Intent, last.Observation); e != nil {
						return false
					}
				}
				required[op] = true
			}
		}
		for _, present := range required {
			if !present {
				return false
			}
		}
	} else {
		outcome, cause := delivery.ClassifyContributionNonSuccess(h, r.Candidate)
		if outcome == "" || r.Outcome != outcome || r.Cause != cause {
			return false
		}
	}
	return evidence.ValidateContributionFinalBinding(completion.Provenance, *completion.Artifact, completion.Receipts)
}
