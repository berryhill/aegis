package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/berryhill/aegis/internal/loop"
	"path/filepath"
)

// validateRecord reloads immutable acceptance and candidate records and checks all
// content-addressed inputs. Mutable progress alone is never delivery evidence.
func (e *ManagedContribution) validateRecord(id string, r ContributionRecord) error {
	bad := errors.New("contribution evidence chain mismatch")
	if r.Policy.Validate() != nil || r.RunID != id || int(r.Consumed) < len(r.Passes) {
		return bad
	}
	if r.Frozen != nil {
		v := *r.Frozen
		wire, err := e.DB.Get([]byte("contribution/candidate/" + v.Digest))
		expected, _ := json.Marshal(v)
		if err != nil || !bytes.Equal(wire, expected) || v.Toolchain != r.FrozenToolchain || v.Contract != r.Contract || v.QueueAttempt != r.QueueAttempt || v.Pass != r.Consumed || int(r.Consumed) != len(r.Passes)+1 || v.Policy != contributionPolicyDigest(r.Policy) || !validContributionRole(v.Builder) || previousBytes(e.DB, v.Executable) == nil || previousBytes(e.DB, v.Toolchain) == nil {
			return bad
		}
		d := v.Digest
		v.Digest = ""
		expected, _ = json.Marshal(v)
		if digest(expected) != d {
			return bad
		}
		source := v.Source
		d = source.Digest
		source.Digest = ""
		expected, _ = json.Marshal(source)
		if digest(expected) != d {
			return bad
		}
	}
	if r.Baseline.OutputDigest != "" {
		wire, err := e.DB.Get([]byte("contribution/baseline/" + digest([]byte(id))))
		expected, _ := json.Marshal(r.Baseline)
		if err != nil || !bytes.Equal(wire, expected) || previousBytes(e.DB, r.Baseline.OutputDigest) == nil {
			return bad
		}
	}
	for i, p := range r.Passes {
		b, err := e.DB.Get([]byte(fmt.Sprintf("contribution/acceptance/%s/%d", digest([]byte(id)), i+1)))
		expected, _ := json.Marshal(p)
		if err != nil || !bytes.Equal(b, expected) {
			return bad
		}
		v := p.Candidate
		if v.Toolchain != r.FrozenToolchain {
			return bad
		}
		b, err = e.DB.Get([]byte("contribution/candidate/" + v.Digest))
		expected, _ = json.Marshal(v)
		if err != nil || !bytes.Equal(b, expected) {
			return bad
		}
		d := v.Digest
		v.Digest = ""
		expected, _ = json.Marshal(v)
		if digest(expected) != d || v.Policy != contributionPolicyDigest(r.Policy) || len(v.CandidateCommit) != 40 || len(v.CandidateTree) != 40 || v.Contract != r.Contract || v.QueueAttempt != r.QueueAttempt || int(v.Pass) != i+1 {
			return bad
		}
		source := v.Source
		d = source.Digest
		source.Digest = ""
		expected, _ = json.Marshal(source)
		if digest(expected) != d {
			return bad
		}
		output := previousBytes(e.DB, p.Check.OutputDigest)
		observations := criterionObservations(output, r.Policy.Criteria)
		actual, _ := json.Marshal(p.Check.Criteria)
		wanted, _ := json.Marshal(observations)
		if !bytes.Equal(actual, wanted) || !independentContributionRole(v.Builder, p.Tester) {
			return bad
		}
		if len(p.Check.RequiredTests) != len(r.Policy.Criteria) {
			return bad
		}
		for j, criterion := range r.Policy.Criteria {
			if p.Check.RequiredTests[j] != criterion.Test {
				return bad
			}
		}
		for _, prior := range r.Passes[:i] {
			if prior.Tester.SessionID == p.Tester.SessionID || prior.Tester.AuthorityID == p.Tester.AuthorityID || prior.Tester.RuntimeSessionID == p.Tester.RuntimeSessionID {
				return bad
			}
		}
		if p.Check.Outcome == "passed" && !requiredTestsPassed(output, p.Check.RequiredTests) {
			return bad
		}
		if p.Check.Outcome == "passed" && e.validateReview(p.Candidate, r.Policy, p.Check.Review) != nil {
			return bad
		}
		if previousBytes(e.DB, v.Toolchain) == nil {
			return bad
		}
		if previousBytes(e.DB, v.Executable) == nil || output == nil {
			return bad
		}
		if !validContributionRole(p.Tester) || !validContributionRole(v.Builder) {
			return bad
		}
	}
	if r.State == "candidate_accepted" && (len(r.Passes) == 0 || r.Passes[len(r.Passes)-1].Check.Outcome != "passed") {
		return bad
	}
	return nil
}

func contributionPolicyDigest(p loop.ContributionExecutionPolicy) string {
	b, _ := json.Marshal(p)
	return digest(b)
}

var ErrContributionHandoffDenied = errors.New("contribution handoff dependencies denied")

// AcceptedCandidate is diagnostic readback, not a source custody lease or final
// disposition. Use WithAcceptedCandidate for a synchronous downstream consumer.
// The caller must supply the currently admitted exact immutable contract/policy.
func (e *ManagedContribution) AcceptedCandidate(run, contract string, policy loop.ContributionExecutionPolicy) (ContributionCandidate, error) {
	if e == nil || e.DB == nil || e.Admit == nil || !filepath.IsAbs(e.GoBinary) {
		return ContributionCandidate{}, ErrContributionHandoffDenied
	}
	r, err := e.Read(run)
	if err != nil {
		return ContributionCandidate{}, err
	}
	if r.State != "candidate_accepted" || r.Contract != contract || contributionPolicyDigest(r.Policy) != contributionPolicyDigest(policy) {
		return ContributionCandidate{}, errors.New("exact accepted candidate policy required")
	}
	v := r.Passes[len(r.Passes)-1].Candidate
	if err = e.revalidate(context.Background(), loop.VerifiedImplementation{Workspace: r.Workspace}, policy, v); err != nil {
		return ContributionCandidate{}, err
	}
	return v, nil
}
