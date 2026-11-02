package implementation

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// VerifyRecoveryRecord validates persisted evidence without executing commands,
// changing progress, or turning a reserved/unknown outcome into a known one.
func VerifyRecoveryRecord(db Store, k, b []byte) error {
	bad := errors.New("unsupported or corrupt implementation recovery record")
	canonical := func(v any) error {
		if json.Unmarshal(b, v) != nil {
			return bad
		}
		wire, e := json.Marshal(v)
		if e != nil || !bytes.Equal(wire, b) {
			return bad
		}
		return nil
	}
	s := string(k)
	switch {
	case strings.HasPrefix(s, "contribution/base-integration/"):
		var v ContributionBaseIntegration
		if canonical(&v) != nil || s != "contribution/base-integration/"+digest(b) {
			return bad
		}
		return nil
	case strings.HasPrefix(s, "implementation/blob/"):
		if s != "implementation/blob/"+digest(b) {
			return bad
		}
		return nil
	case strings.HasPrefix(s, "contribution/execution/"):
		var r ContributionRecord
		if canonical(&r) != nil || !bytes.Equal(k, contributionKey(r.RunID)) {
			return bad
		}
		return (&ManagedContribution{DB: db}).validateRecord(r.RunID, r)
	case strings.HasPrefix(s, "contribution/workspace/"):
		r, e := (&ManagedContribution{DB: db}).Read(string(b))
		if e != nil || s != "contribution/workspace/"+digest([]byte(r.Workspace)) {
			return bad
		}
		return nil
	case strings.HasPrefix(s, "contribution/candidate/"):
		var v ContributionCandidate
		if canonical(&v) != nil {
			return bad
		}
		d := v.Digest
		v.Digest = ""
		wire, _ := json.Marshal(v)
		if digest(wire) != d || s != "contribution/candidate/"+d {
			return bad
		}
		return nil
	case strings.HasPrefix(s, "contribution/review/"):
		if s != "contribution/review/"+digest(b) {
			return bad
		}
		var v ContributionReviewLedger
		return canonical(&v)
	case strings.HasPrefix(s, "contribution/baseline/"):
		var v ContributionCheck
		return canonical(&v)
	case strings.HasPrefix(s, "contribution/acceptance/"):
		var v ContributionPass
		return canonical(&v)
	default:
		return bad
	}
}
