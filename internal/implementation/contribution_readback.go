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

	// Reload independently of mutable baseline fields. Preparation may legitimately
	// have no baseline yet, but progress beyond it must retain actual RED proof.
	completedBaseline := r.Consumed > 0 || len(r.Passes) > 0 || r.Expected != nil || r.BoundarySource != "" || r.BoundaryIndex != "" ||
		r.State == "phase_boundary" || r.State == "candidate_accepted" || r.State == "candidate_budget_exhausted"
	wire, baselineErr := e.DB.Get([]byte("contribution/baseline/" + digest([]byte(id))))
	emptyBaseline, _ := json.Marshal(ContributionCheck{})
	expectedBaseline, _ := json.Marshal(r.Baseline)
	if !errors.Is(baselineErr, ErrNotFound) || completedBaseline || !bytes.Equal(emptyBaseline, expectedBaseline) {
		if baselineErr != nil || !bytes.Equal(wire, expectedBaseline) {
			return bad
		}
		output := previousBytes(e.DB, r.Baseline.OutputDigest)
		if output == nil || !exactContributionTests(r.Baseline, r.Policy) || r.Baseline.Review != "" || len(r.Baseline.Criteria) != 0 {
			return bad
		}
		if r.Baseline.Outcome != "failed" && r.Baseline.Outcome != "passed" {
			return bad
		}
		if r.Baseline.Outcome == "passed" && !requiredTestsPassed(output, r.Baseline.RequiredTests) {
			return bad
		}
		if r.Baseline.Outcome == "failed" && requiredTestsPassed(output, r.Baseline.RequiredTests) {
			return bad
		}
		if completedBaseline && (r.Baseline.Outcome != "failed" || !requiredRegressionFailed(output, r.Baseline.RequiredTests)) {
			return bad
		}
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
		if !exactContributionTests(p.Check, r.Policy) || (p.Check.Outcome != "passed" && p.Check.Outcome != "failed") ||
			(p.Check.Outcome == "failed" && requiredTestsPassed(output, p.Check.RequiredTests)) {
			return bad
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
	if err := e.validateDeliveryRepairs(r); err != nil {
		return err
	}
	if r.Integration != nil {
		data, _ := json.Marshal(r.Integration)
		stored, err := e.DB.Get([]byte("contribution/base-integration/" + digest(data)))
		if err != nil || !bytes.Equal(stored, data) || r.Integration.OriginalBase != r.Base {
			return bad
		}
		linked := false
		for _, repair := range r.DeliveryRepairs {
			if repair.Class == "base_advanced" && repair.CurrentBase == r.Integration.CurrentBase && repair.Candidate == r.Integration.Predecessor {
				linked = true
			}
		}
		if !linked {
			return bad
		}
	}
	// The current-base pin is derived from the immutable admitted integration,
	// never trusted as a standalone candidate string. Earlier passes keep the
	// original base and cannot claim the successor integration.
	checkBase := func(v ContributionCandidate) bool {
		expected := ""
		for _, repair := range r.DeliveryRepairs {
			if repair.Class == "base_advanced" && repair.AfterPass < v.Pass {
				if r.Integration == nil || repair.Candidate != r.Integration.Predecessor || repair.CurrentBase != r.Integration.CurrentBase || r.Integration.OriginalBase != v.Base {
					return false
				}
				expected = repair.CurrentBase
			}
		}
		return v.CurrentBase == expected
	}
	if r.Frozen != nil && !checkBase(*r.Frozen) {
		return bad
	}
	for _, pass := range r.Passes {
		if !checkBase(pass.Candidate) {
			return bad
		}
	}
	for i, pass := range r.Passes {
		if pass.Check.Outcome != "passed" || i == len(r.Passes)-1 {
			continue
		}
		linked := false
		for _, repair := range r.DeliveryRepairs {
			linked = linked || int(repair.AfterPass) == i+1
		}
		if !linked {
			return bad
		}
	}
	return nil
}

func contributionPolicyDigest(p loop.ContributionExecutionPolicy) string {
	b, _ := json.Marshal(p)
	return digest(b)
}

func exactContributionTests(check ContributionCheck, policy loop.ContributionExecutionPolicy) bool {
	if len(check.RequiredTests) != len(policy.Criteria) {
		return false
	}
	for i, criterion := range policy.Criteria {
		if check.RequiredTests[i] != criterion.Test {
			return false
		}
	}
	return true
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
