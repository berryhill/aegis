package app

import (
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/evidence"
)

// Pure cross-binding predicate used only after canonical record/blob reload.
func contributionResultReceiptMatches(d disposition.Record, a evidence.RuntimeArtifact, r evidence.VerificationReceipt) bool {
	return len(d.ReceiptIDs) == 1 && r.ID == d.ReceiptIDs[0] && r.Validate() == nil && r.ArtifactID == a.ID && r.AttemptID == a.AttemptID && r.RunID == a.RunID && r.ActionID == a.ActionID && r.OwnerID == a.OwnerID && r.AuthorityContextID == a.AuthorityContextID && r.AuthorityContextDigest == a.AuthorityContextDigest && r.MediaType == a.MediaType && r.Outcome == evidence.Passed && r.ExpectedDigest == a.Digest && r.ObservedDigest == a.Digest && r.VerifierID == "aegis-contribution-verifier" && r.Claim == evidence.ContributionFinalClaim && r.PolicyVersion == evidence.ContributionFinalPolicy
}
