package app

import (
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
	"strings"
	"testing"
	"time"
)

func TestContributionResultReceiptBindings(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	a := evidence.RuntimeArtifact{ID: "artifact", AttemptID: "attempt", OwnerID: "controller", RunID: "execution", ActionID: "execution", AuthorityContextID: "authority", AuthorityContextDigest: digest, Digest: digest, MediaType: evidence.ContributionFinalMediaType}
	d := disposition.Record{ReceiptIDs: []string{"receipt"}}
	r := evidence.VerificationReceipt{ID: "receipt", AttemptID: a.AttemptID, ArtifactID: a.ID, ActionID: a.ActionID, RunID: a.RunID, OwnerID: a.OwnerID, AuthorityContextID: a.AuthorityContextID, AuthorityContextDigest: a.AuthorityContextDigest, VerifierID: "aegis-contribution-verifier", PolicyVersion: evidence.ContributionFinalPolicy, Claim: evidence.ContributionFinalClaim, MediaType: a.MediaType, ExpectedDigest: digest, ObservedDigest: digest, Outcome: evidence.Passed, EvidenceRef: digest, ObservedAt: time.Now()}
	if !contributionResultReceiptMatches(d, a, r) {
		t.Fatal("exact synthetic binding rejected")
	}
	for _, kind := range []string{"id", "run", "attempt", "action", "owner", "authority", "media", "digest", "policy", "state"} {
		bad := r
		switch kind {
		case "id":
			bad.ID = "other"
		case "run":
			bad.RunID = "other"
		case "attempt":
			bad.AttemptID = "other"
		case "action":
			bad.ActionID = "other"
		case "owner":
			bad.OwnerID = "other"
		case "authority":
			bad.AuthorityContextDigest = "sha256:" + strings.Repeat("b", 64)
		case "media":
			bad.MediaType = "text/plain"
		case "digest":
			bad.ExpectedDigest = "sha256:" + strings.Repeat("c", 64)
		case "policy":
			bad.PolicyVersion = "generic"
		case "state":
			bad.Outcome = evidence.Failed
			bad.FailureCategory = "failed"
		}
		if contributionResultReceiptMatches(d, a, bad) {
			t.Fatal("substituted result accepted", kind)
		}
	}
}
