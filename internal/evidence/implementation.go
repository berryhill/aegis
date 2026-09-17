package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/store"
)

// ImplementationCheck reloads controller-owned evidence for one exact contract.
// The outer composition layer supplies this port; evidence never imports a runner.
type ImplementationCheck struct {
	ContractDigest string
	PolicyVersion  string
	Reload         func() ([]byte, error)
}

// VerifyImplementation issues evidence only from independently reloaded kernel custody.
func (v *BlobVerifier) VerifyImplementation(ctx context.Context, artifact RuntimeArtifact, verification ImplementationCheck) (VerificationReceipt, CompletionProvenance, error) {
	if verification.Reload == nil || verification.ContractDigest == "" || verification.PolicyVersion == "" {
		return VerificationReceipt{}, CompletionProvenance{}, errors.New("implementation verifier required")
	}
	check := func() error {
		output, err := verification.Reload()
		if err != nil {
			return err
		}
		actual, err := v.store.GetBlob(artifact.ContentRef)
		if err != nil || !bytes.Equal(actual, output) || sha256Reference(output) != artifact.Digest {
			return errors.New("implementation output binding mismatch")
		}
		return nil
	}
	if err := check(); err != nil {
		return VerificationReceipt{}, CompletionProvenance{}, err
	}
	r := VerificationReceipt{ID: store.ID("evidence"), AttemptID: artifact.AttemptID, ArtifactID: artifact.ID, ActionID: artifact.ActionID, RunID: artifact.RunID, OwnerID: artifact.OwnerID, AuthorityContextID: artifact.AuthorityContextID, AuthorityContextDigest: artifact.AuthorityContextDigest, VerifierID: "aegis.implementation.verifier", PolicyVersion: verification.PolicyVersion, Claim: "verified-implementation", MediaType: "application/json", ExpectedDigest: artifact.Digest, ObservedDigest: artifact.Digest, Outcome: Passed, ObservedAt: v.now().UTC()}
	wire, err := json.Marshal(r)
	if err != nil {
		return r, CompletionProvenance{}, err
	}
	r.EvidenceRef, err = v.store.PutBlob(wire)
	if err != nil {
		return r, CompletionProvenance{}, err
	}
	p, err := v.AuthorizeCompletion(ctx, artifact, []VerificationReceipt{r})
	if err != nil {
		return r, p, err
	}
	p.implementationContract = verification.ContractDigest
	p.implementationCheck = check
	return r, p, err
}
func ValidateImplementationProvenance(p CompletionProvenance, contract string, policyVersion string, artifact RuntimeArtifact, receipts []VerificationReceipt) bool {
	return p.implementationCheck != nil && p.implementationContract == contract && len(receipts) == 1 && receipts[0].VerifierID == "aegis.implementation.verifier" && receipts[0].Claim == "verified-implementation" && receipts[0].PolicyVersion == policyVersion && ValidateCompletionProvenance(p, artifact, receipts)
}
