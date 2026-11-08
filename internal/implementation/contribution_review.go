package implementation

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/loop"
)

type AssetReviewEvidence struct{ ID, Scope, Path, Status, Digest, Reason string }
type ContributionReviewLedger struct {
	Check                     string `json:"check,omitempty"`
	Candidate, Source, Policy string
	Assets                    []AssetReviewEvidence
}

// Historical policies retain exact-content approval. Engineering policies bind
// actual immutable security/launch journeys, not a builder/model PASS or advance
// approval of future source bytes. The ledger records changed/unaffected assets.
func reviewCandidate(v ContributionCandidate, p loop.ContributionExecutionPolicy) (ContributionReviewLedger, error) {
	l := ContributionReviewLedger{Candidate: v.Digest, Source: v.Source.Digest, Policy: contributionPolicyDigest(p)}
	bad := errors.New("incomplete or mismatched approved asset review")
	if p.Validate() != nil || v.Policy != l.Policy {
		return l, bad
	}
	files := map[string]string{}
	for _, f := range v.Source.Files {
		files[f.Path] = digest(f.Content)
	}
	for _, a := range p.ReviewAssets {
		r := AssetReviewEvidence{ID: a.ID, Scope: a.Scope, Path: a.Path, Reason: a.NonApplicable}
		if a.NonApplicable != "" {
			r.Status = "reviewed_nonapplicable"
		} else {
			r.Digest = files[a.Path]
			r.Status = "missing"
			if r.Digest != "" {
				r.Status = "changed"
				if r.Digest == a.BaselineDigest {
					r.Status = "unaffected"
				}
			}
			if r.Digest == "" || (p.ReviewMode == "" && r.Digest != a.ApprovedDigest) {
				return l, bad
			}
		}
		l.Assets = append(l.Assets, r)
	}
	return l, nil
}
func (e *ManagedContribution) reviewJourneysPassed(output string, p loop.ContributionExecutionPolicy) bool {
	tests := []loop.RequiredGoTest{}
	for _, c := range p.Criteria {
		tests = append(tests, c.Test)
	}
	wire := previousBytes(e.DB, output)
	return len(wire) > 0 && requiredTestsPassed(wire, tests)
}
func (e *ManagedContribution) persistReview(v ContributionCandidate, p loop.ContributionExecutionPolicy, checks ...ContributionCheck) (string, error) {
	l, err := reviewCandidate(v, p)
	if err != nil {
		return "", err
	}
	if p.ReviewMode == "issue-journeys.v1" {
		if len(checks) != 1 || checks[0].Outcome != "passed" || !e.reviewJourneysPassed(checks[0].OutputDigest, p) {
			return "", errors.New("actual independent review journey evidence required")
		}
		l.Check = checks[0].OutputDigest
	}
	b, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	d := digest(b)
	return d, e.DB.Create([]byte("contribution/review/"+d), b)
}
func (e *ManagedContribution) validateReview(v ContributionCandidate, p loop.ContributionExecutionPolicy, d string, outputs ...string) error {
	l, err := reviewCandidate(v, p)
	if err != nil {
		return err
	}
	got, err := e.DB.Get([]byte("contribution/review/" + d))
	if p.ReviewMode == "issue-journeys.v1" {
		var retained ContributionReviewLedger
		if err != nil || json.Unmarshal(got, &retained) != nil || len(outputs) != 1 || retained.Check != outputs[0] || !e.reviewJourneysPassed(retained.Check, p) {
			return errors.New("missing independent review journeys")
		}
		l.Check = retained.Check
	}
	want, _ := json.Marshal(l)
	if err != nil || digest(got) != d || !bytes.Equal(got, want) {
		return errors.New("immutable candidate review ledger mismatch")
	}
	return nil
}
