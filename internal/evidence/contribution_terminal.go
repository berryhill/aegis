package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/reference"
)

const ContributionFinalPolicy = "aegis.contribution.final.v1"
const ContributionFinalMediaType = "application/vnd.aegis.contribution-result+json"
const ContributionFinalClaim = "whole-contribution-final"

// ContributionTerminalReport is immutable terminal content. Missing evidence on
// non-success is represented by omission, never success-shaped dummy references.
type ContributionTerminalReport struct {
	Current                                                                    *ContributionCurrentCompletion `json:"current_completion,omitempty"`
	Schema                                                                     string                         `json:"schema"`
	Owner, Key, Run, Contract, Scope, Outcome, Reason                          string
	Candidate, Policy, Native, History, Continuity, Control, PreClosure, Issue string   `json:",omitempty"`
	MergeRefs                                                                  []string `json:"merge_refs"`
	Claim, Attempt                                                             string
	Cause                                                                      string `json:"cause,omitempty"`
}

// Actual current role/continuation are independently reloaded by the store.
type ContributionCurrentCompletion struct {
	Authority, OriginalAuthority, Role, Continuation reference.DigestRef
	Epoch                                            uint64
	Mandate, OriginalMandate, Stanza                 string
}

func (r ContributionTerminalReport) Validate() error {
	if r.Current != nil {
		p := r.Current
		if p.Authority.Validate() != nil || p.OriginalAuthority.Validate() != nil || p.Role.Validate() != nil || p.Continuation.Validate() != nil || p.Authority == p.OriginalAuthority || p.Epoch == 0 || p.Mandate == "" || p.OriginalMandate == "" || p.Stanza == "" {
			return ErrContributionEvidence
		}
	}
	if r.Schema != ContributionFinalPolicy || r.Owner == "" || r.Key == "" || r.Run == "" || !validDigest(r.Contract) || r.Reason == "" || r.MergeRefs == nil {
		return ErrContributionEvidence
	}
	switch r.Scope {
	case "local-only", "no-publish", "no-merge", "verified-protected-base-merge":
	default:
		return ErrContributionEvidence
	}
	switch r.Outcome {
	case "succeeded":
		if r.Scope != "verified-protected-base-merge" || r.Issue == "" || r.PreClosure == "" || len(r.MergeRefs) == 0 {
			return ErrContributionEvidence
		}
	case "narrower-target-completed":
		if r.Scope == "verified-protected-base-merge" || r.Issue != "" || len(r.MergeRefs) != 0 {
			return ErrContributionEvidence
		}
	case "merged-unresolved":
		if len(r.MergeRefs) == 0 {
			return ErrContributionEvidence
		}
	case "failed", "denied", "cancelled", "expired", "exhausted", "unknown":
		if len(r.MergeRefs) != 0 {
			return ErrContributionEvidence
		}
	default:
		return ErrContributionEvidence
	}
	if r.Outcome == "succeeded" || r.Outcome == "narrower-target-completed" {
		for _, d := range []string{r.Candidate, r.Policy, r.Native, r.History, r.Continuity} {
			if !validDigest(d) {
				return ErrContributionEvidence
			}
		}
		if r.Claim == "" || r.Attempt == "" {
			return ErrContributionEvidence
		}
	}
	for _, d := range r.MergeRefs {
		if !validDigest(d) {
			return ErrContributionEvidence
		}
	}
	return nil
}
func DecodeContributionTerminalReport(ref string, b []byte) (ContributionTerminalReport, error) {
	var r ContributionTerminalReport
	if sha256Reference(b) != ref || reference.RejectDuplicateObjectKeys(b) != nil || strictContributionJSON(b, &r) != nil || r.Validate() != nil {
		return r, ErrContributionEvidence
	}
	return r, nil
}

// SealContributionReportReceipt seals integrity of an already producer-verified
// terminal report, not producer authority. The dedicated canonical transaction
// must also revalidate contribution anchors; generic completion cannot promote it.
func (v *BlobVerifier) SealContributionReportReceipt(ctx context.Context, a RuntimeArtifact, r VerificationReceipt) (CompletionProvenance, error) {
	if a.MediaType != ContributionFinalMediaType || r.Claim != ContributionFinalClaim || r.VerifierID != "aegis-contribution-verifier" || r.PolicyVersion != ContributionFinalPolicy {
		return CompletionProvenance{}, errors.New("contribution receipt required")
	}
	b, e := v.store.GetBlob(a.ContentRef)
	if e != nil {
		return CompletionProvenance{}, e
	}
	report, e := DecodeContributionTerminalReport(a.ContentRef, b)
	if e != nil || report.Attempt != a.AttemptID {
		return CompletionProvenance{}, ErrContributionEvidence
	}
	provenance, e := v.AuthorizeCompletion(ctx, a, []VerificationReceipt{r})
	if e != nil {
		return provenance, e
	}
	provenance.contributionFinal = a.Digest
	return provenance, nil
}
func ValidateContributionFinalBinding(p CompletionProvenance, a RuntimeArtifact, r []VerificationReceipt) bool {
	return p.contributionFinal == a.Digest && p.contributionFinal != "" && len(r) == 1 && r[0].Claim == ContributionFinalClaim && r[0].PolicyVersion == ContributionFinalPolicy && ValidateCompletionProvenance(p, a, r)
}
func MarshalContributionTerminalReport(r ContributionTerminalReport) ([]byte, error) {
	if r.Validate() != nil {
		return nil, ErrContributionEvidence
	}
	return json.Marshal(r)
}
