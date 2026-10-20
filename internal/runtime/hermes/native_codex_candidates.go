package hermes

import "regexp"

// The public selector is a domain-separated digest of the exact native ID,
// never a label, token fingerprint or native caller-controlled string.
var nativeCredentialIDPattern = regexp.MustCompile(`^nc-[a-f0-9]{64}$`)

func ValidNativeCredentialID(id string) bool { return nativeCredentialIDPattern.MatchString(id) }
func nativeCredentialSelector(ids []string) (string, error) {
	if len(ids) > 1 {
		return "", ErrProviderAuthUnauthorized
	}
	if len(ids) == 0 || ids[0] == "" {
		return "", nil
	}
	if !ValidNativeCredentialID(ids[0]) {
		return "", ErrProviderAuthUnauthorized
	}
	return ids[0], nil
}

type NativeCodexCandidate struct {
	CredentialID       string `json:"credential_id,omitempty"`
	AccountFingerprint string `json:"account_fingerprint,omitempty"`
	Eligible           bool   `json:"eligible"`
	Reason             string `json:"reason"`
}
type NativeCodexCandidates struct {
	ProfileHome            string                 `json:"profile_home"`
	SelectionPolicy        string                 `json:"selection_policy"`
	DistinctEligibleGrants int                    `json:"distinct_eligible_grants"`
	Candidates             []NativeCodexCandidate `json:"candidates"`
}
