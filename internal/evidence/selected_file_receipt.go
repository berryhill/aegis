package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/store"
)

const DoerFileVerifierID = "aegis.doer.selected-file.verifier"
const DoerFileVerifierPolicyV1 = "aegis.doer.selected-file.v1"

// VerifySelectedFileArtifact binds the actual selected file bytes in the
// content-addressed blob store to one exact controller-pinned assertion. The
// callback is re-run at final repository completion; a model cannot mint it.
func (v *BlobVerifier) VerifySelectedFileArtifact(ctx context.Context, artifact RuntimeArtifact, workspace string, policy SelectedFilePolicy, policyDigest, contractDigest, producedDigest string, binding SelectedFileBinding) (VerificationReceipt, CompletionProvenance, error) {
	if v == nil || binding.Validate() != nil || !validDigest(contractDigest) || !validDigest(producedDigest) || artifact.Validate() != nil || artifact.MediaType != "application/octet-stream" ||
		artifact.AttemptID != binding.AttemptID || artifact.ActionID != binding.ActionID || artifact.RunID != binding.RunID || artifact.OwnerID != binding.OwnerID ||
		artifact.AuthorityContextID != binding.AuthorityContextID || artifact.AuthorityContextDigest != binding.AuthorityContextDigest {
		return VerificationReceipt{}, CompletionProvenance{}, errors.New("selected-file artifact binding invalid")
	}
	check := func() error {
		observation, err := VerifySelectedFile(ctx, workspace, policy, policyDigest, binding)
		if err != nil || observation.Outcome != Passed || observation.ContentDigest != artifact.Digest || observation.ContentDigest != producedDigest {
			return errors.New("selected-file assertion no longer passes")
		}
		stored, err := v.store.GetBlob(artifact.ContentRef)
		if err != nil || !bytes.Equal(stored, observation.Content) {
			return errors.New("selected-file content differs from stored artifact")
		}
		return nil
	}
	if err := check(); err != nil {
		return VerificationReceipt{}, CompletionProvenance{}, err
	}
	receipt := VerificationReceipt{ID: store.ID("evidence"), AttemptID: artifact.AttemptID, ArtifactID: artifact.ID, ActionID: artifact.ActionID, RunID: artifact.RunID, OwnerID: artifact.OwnerID,
		AuthorityContextID: artifact.AuthorityContextID, AuthorityContextDigest: artifact.AuthorityContextDigest,
		VerifierID: DoerFileVerifierID, PolicyVersion: DoerFileVerifierPolicyV1, Claim: "selected-file-verified", MediaType: artifact.MediaType,
		ExpectedDigest: artifact.Digest, ObservedDigest: artifact.Digest, Outcome: Passed, ObservedAt: v.now().UTC()}
	wire, err := json.Marshal(receipt)
	if err != nil {
		return VerificationReceipt{}, CompletionProvenance{}, err
	}
	receipt.EvidenceRef, err = v.store.PutBlob(wire)
	if err != nil {
		return VerificationReceipt{}, CompletionProvenance{}, err
	}
	proof, err := v.AuthorizeCompletion(ctx, artifact, []VerificationReceipt{receipt})
	if err != nil {
		return VerificationReceipt{}, CompletionProvenance{}, err
	}
	proof.selectedFileContract = contractDigest
	proof.selectedFileCheck = check
	return receipt, proof, nil
}

func ValidateSelectedFileProvenance(proof CompletionProvenance, contractDigest string, artifact RuntimeArtifact, receipts []VerificationReceipt) bool {
	return proof.selectedFileCheck != nil && proof.selectedFileContract == contractDigest && len(receipts) == 1 &&
		receipts[0].VerifierID == DoerFileVerifierID && receipts[0].PolicyVersion == DoerFileVerifierPolicyV1 && receipts[0].Claim == "selected-file-verified" &&
		receipts[0].MediaType == "application/octet-stream" && receipts[0].ExpectedDigest == artifact.Digest && receipts[0].ObservedDigest == artifact.Digest &&
		ValidateCompletionProvenance(proof, artifact, receipts)
}
