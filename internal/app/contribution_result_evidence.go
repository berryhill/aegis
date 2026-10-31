package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
)

// Reload only immutable evidence retained by the canonical finalizer. Retrieval
// never re-enters native custody, renews authority, or reconciles remote effects.
// A final receipt authenticates its report, not the availability of its joins.
func (s *Service) reloadContributionResultEvidence(ctx context.Context, c contribution.Contract, r evidence.ContributionTerminalReport) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	read := func(ref string) ([]byte, error) {
		b, err := s.Store.GetBlob(ref)
		if err != nil || contribution.Digest(b) != ref {
			return nil, ErrDenied
		}
		return b, nil
	}
	var policy *ContributionDeliveryPolicy
	if r.Policy != "" {
		b, err := read(r.Policy)
		p, decodeErr := decodeContributionDeliveryPolicy(b, r.Policy)
		if err != nil || decodeErr != nil || p.Binding.OwnerID != c.OwnerID || p.Binding.OwnerID != r.Owner || p.Binding.Key != c.Key || p.Binding.Key != r.Key || p.Binding.Run != c.RunID || p.Binding.Run != r.Run || p.Binding.Contract != c.Digest || p.Binding.Contract != r.Contract || p.Binding.Candidate != r.Candidate || p.Binding.QueueItemID != c.QueueItemID || p.Loop != c.Loop || p.Workspace != c.Worktree || p.BaseHead != c.Base || p.QueueAttempt != r.Attempt {
			return ErrDenied
		}
		policy = &p
	}
	if r.Native != "" {
		b, err := read(r.Native)
		var native map[string]json.RawMessage
		if err != nil || reference.RejectDuplicateObjectKeys(b) != nil || json.Unmarshal(b, &native) != nil {
			return ErrDenied
		}
		text := func(key string) string { var v string; _ = json.Unmarshal(native[key], &v); return v }
		var passes []map[string]json.RawMessage
		if text("schema") != "aegis.contribution.native-acceptance.v1" || text("run_id") != c.RunID || text("contract") != c.Digest || text("queue_attempt") != r.Attempt || json.Unmarshal(native["passes"], &passes) != nil || len(passes) == 0 {
			return ErrDenied
		}
		var candidate orchestration.ContributionCandidate
		if json.Unmarshal(passes[len(passes)-1]["candidate"], &candidate) != nil || candidate.Digest != r.Candidate {
			return ErrDenied
		}
		if policy != nil && (candidate.QueueAttempt != policy.QueueAttempt || candidate.Contract != policy.Binding.Contract || candidate.Base != policy.BaseHead || candidate.CandidateTree != policy.Tree || candidate.Source.Digest != policy.Source || candidate.Policy != policy.CandidatePolicy || candidate.Toolchain != policy.Toolchain) {
			return ErrDenied
		}
	}
	if r.History != "" {
		b, err := read(r.History)
		var history delivery.HistorySnapshot
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if err != nil || reference.RejectDuplicateObjectKeys(b) != nil || decoder.Decode(&history) != nil || decoder.Decode(new(any)) != io.EOF || delivery.ValidateHistorySnapshot(history) != nil || history.Contract.Digest != c.Digest || history.Root.Run != r.Run || history.Root.OwnerID != r.Owner || history.Root.Key != r.Key {
			return ErrDenied
		}
		wire, err := json.Marshal(history)
		if err != nil || !bytes.Equal(wire, b) {
			return ErrDenied
		}
		merges := []string{}
		issueFound := r.Issue == ""
		for _, chain := range history.Histories {
			if policy != nil {
				i := chain[0].Record.Intent
				if i.Candidate == r.Candidate && (i.Reference != policy.Binding.Reference || i.FenceEpoch != policy.Binding.FenceEpoch || i.RoleBinding != policy.Binding.RoleBinding || i.Actor != policy.Actor || i.Base != policy.Binding.Base || i.Branch != policy.Branch || i.BaseHead != policy.BaseHead || i.ExpectedOld != policy.ExpectedOld || i.Policy != r.Policy) {
					return ErrDenied
				}
			}
			for _, entry := range chain {
				if entry.Record.Merge != "" {
					merges = append(merges, entry.Digest)
				}
				if entry.Digest == r.Issue && entry.Record.Intent.Operation == delivery.ReconcileIssue && entry.Record.Outcome == delivery.Confirmed {
					issueFound = true
				}
			}
		}
		want, _ := json.Marshal(r.MergeRefs)
		got, _ := json.Marshal(merges)
		if !issueFound || !bytes.Equal(want, got) {
			return ErrDenied
		}
	}
	if r.PreClosure != "" {
		b, err := read(r.PreClosure)
		var binding struct{ Schema, Owner, Key, Contract, Candidate, Native, History, Continuity, Policy, Claim, Attempt string }
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if err != nil || reference.RejectDuplicateObjectKeys(b) != nil || decoder.Decode(&binding) != nil || decoder.Decode(new(any)) != io.EOF || binding.Schema != "aegis.contribution.preclosure.v1" || binding.Owner != r.Owner || binding.Key != r.Key || binding.Contract != r.Contract || binding.Candidate != r.Candidate || binding.Native != r.Native || binding.Policy != r.Policy || binding.Claim != r.Claim || binding.Attempt != r.Attempt {
			return ErrDenied
		}
	}
	return ctx.Err()
}
