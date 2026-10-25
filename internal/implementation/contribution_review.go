package implementation

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/loop"
)

type AssetReviewEvidence struct{ ID, Scope, Path, Status, Digest, Reason string }
type ContributionReviewLedger struct {
	Candidate, Source, Policy string
	Assets                    []AssetReviewEvidence
}

// A closed exact-content review: approval of these bytes lives in the immutable
// operator policy, never in caller PASS text. This is not a general semantic
// reviewer. Any other bytes require a new policy approval.
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
			if r.Digest != a.ApprovedDigest {
				return l, bad
			}
		}
		l.Assets = append(l.Assets, r)
	}
	return l, nil
}
func (e *ManagedContribution) persistReview(v ContributionCandidate, p loop.ContributionExecutionPolicy) (string, error) {
	l, err := reviewCandidate(v, p)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	d := digest(b)
	err = e.DB.Create([]byte("contribution/review/"+d), b)
	return d, err
}
func (e *ManagedContribution) validateReview(v ContributionCandidate, p loop.ContributionExecutionPolicy, d string) error {
	l, err := reviewCandidate(v, p)
	if err != nil {
		return err
	}
	want, _ := json.Marshal(l)
	got, err := e.DB.Get([]byte("contribution/review/" + d))
	if err != nil || digest(got) != d || !bytes.Equal(got, want) {
		return errors.New("immutable candidate review ledger mismatch")
	}
	return nil
}
