package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/loop"
)

const ContributionAcceptanceSchema = "aegis.contribution.native-acceptance.v1"

// ContributionAcceptanceEvidence is a private-custody export of the existing
// native producer, not a second candidate format and not final contribution
// success. It deliberately contains the actual typed candidate and policy.
// Source bytes/test output must never be published to GitHub or model broadcasts.
type ContributionAcceptanceEvidence struct {
	Schema       string                           `json:"schema"`
	RunID        string                           `json:"run_id"`
	Contract     string                           `json:"contract"`
	QueueAttempt string                           `json:"queue_attempt"`
	Consumed     uint8                            `json:"consumed"`
	Policy       loop.ContributionExecutionPolicy `json:"policy"`
	Baseline     ContributionCheck                `json:"baseline"`
	Passes       []ContributionPass               `json:"passes"`
}

// ReloadAcceptedEvidence independently reconstructs native evidence from the
// actual kernel owner. It never consumes caller-supplied PASS/receipt JSON or a
// function that can accept arbitrary evidence. The caller must retain actual
// source custody and separately validate role authority, stop/fencing and remote
// delivery through their owning services; this method grants none of those.
func (e *ManagedContribution) ReloadAcceptedEvidence(ctx context.Context, run, contract, attempt, candidate string, policy loop.ContributionExecutionPolicy) ([]byte, error) {
	bad := errors.New("native contribution acceptance evidence invalid")
	if e == nil || e.DB == nil || e.Admit == nil || ctx.Err() != nil || attempt == "" || candidate == "" {
		return nil, bad
	}
	if err := e.admit(ctx, "candidate-verification"); err != nil {
		return nil, err
	}
	r, err := e.Read(run)
	if err != nil {
		return nil, err
	}
	// Check canonical producer bytes as well as their independently reloaded
	// immutable pass/candidate/review records. Go's permissive JSON decoder alone
	// would silently accept duplicate keys, aliases, unknown fields and nulls.
	stored, err := e.DB.Get(contributionKey(run))
	canonical, marshalErr := json.Marshal(r)
	if err != nil || marshalErr != nil || !bytes.Equal(stored, canonical) || r.RunID != run || r.Contract != contract || r.QueueAttempt != attempt ||
		r.State != "candidate_accepted" || len(r.Passes) == 0 || int(r.Consumed) != len(r.Passes) || int(r.Consumed) > 2 ||
		contributionPolicyDigest(r.Policy) != contributionPolicyDigest(policy) || r.Baseline.Outcome != "failed" {
		return nil, bad
	}
	last := r.Passes[len(r.Passes)-1]
	if last.Candidate.Digest != candidate || last.Check.Outcome != "passed" || len(last.Check.Criteria) != len(policy.Criteria) {
		return nil, bad
	}
	for i, criterion := range policy.Criteria {
		o := last.Check.Criteria[i]
		if o.Criterion != criterion || o.Status != CriterionPassed || o.FailureCode != "" || o.Reproduce != criterion.Test || o.Expected != "required_test_pass" {
			return nil, bad
		}
	}
	if err = e.revalidate(ctx, loop.VerifiedImplementation{Workspace: r.Workspace}, policy, last.Candidate); err != nil {
		return nil, err
	}
	out, err := json.Marshal(ContributionAcceptanceEvidence{ContributionAcceptanceSchema, run, contract, attempt, r.Consumed, r.Policy, r.Baseline, r.Passes})
	if err != nil {
		return nil, err
	}
	// Fresh immutable reload at the final export boundary; candidate bytes alone
	// cannot replace the original trusted native observation/review provenance.
	after, err := e.Read(run)
	wire, _ := json.Marshal(after)
	if err != nil || !bytes.Equal(wire, canonical) {
		return nil, bad
	}
	if err = e.admit(ctx, "candidate-verification"); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
