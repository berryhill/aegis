package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/store"
)

// VerifyReusableDoerArtifact seals the exact run contract and selected bytes in
// a JSON artifact. Completion rechecks both content-addressed blobs and the file.
func (v *BlobVerifier) VerifyReusableDoerArtifact(ctx context.Context, file RuntimeArtifact, workspace string, policy SelectedFilePolicy, policyDigest, contractDigest, producedDigest string, binding SelectedFileBinding) (RuntimeArtifact, VerificationReceipt, CompletionProvenance, error) {
	if v == nil || file.Validate() != nil || file.MediaType != "application/octet-stream" || binding.Validate() != nil ||
		!validDigest(contractDigest) || !validDigest(producedDigest) || file.Digest != producedDigest ||
		file.AttemptID != binding.AttemptID || file.ActionID != binding.ActionID || file.RunID != binding.RunID || file.OwnerID != binding.OwnerID ||
		file.AuthorityContextID != binding.AuthorityContextID || file.AuthorityContextDigest != binding.AuthorityContextDigest {
		return RuntimeArtifact{}, VerificationReceipt{}, CompletionProvenance{}, errors.New("reusable Doer file binding invalid")
	}
	wire, err := json.Marshal(struct {
		ContractDigest string `json:"contract_digest"`
		FileDigest     string `json:"file_digest"`
		FileRef        string `json:"file_ref"`
	}{contractDigest, producedDigest, file.ContentRef})
	if err != nil {
		return RuntimeArtifact{}, VerificationReceipt{}, CompletionProvenance{}, err
	}
	ref, err := v.store.PutBlob(wire)
	if err != nil {
		return RuntimeArtifact{}, VerificationReceipt{}, CompletionProvenance{}, err
	}
	artifact := file
	artifact.Digest, artifact.ContentRef, artifact.MediaType = ref, ref, "application/json"
	check := func() error {
		observation, err := VerifySelectedFile(ctx, workspace, policy, policyDigest, binding)
		if err != nil || observation.Outcome != Passed || observation.ContentDigest != producedDigest {
			return errors.New("reusable Doer selected file assertion failed")
		}
		storedFile, err := v.store.GetBlob(file.ContentRef)
		if err != nil || !bytes.Equal(storedFile, observation.Content) {
			return errors.New("reusable Doer file differs from stored bytes")
		}
		storedProof, err := v.store.GetBlob(artifact.ContentRef)
		if err != nil || !bytes.Equal(storedProof, wire) {
			return errors.New("reusable Doer proof differs from stored bytes")
		}
		return nil
	}
	if err := check(); err != nil {
		return RuntimeArtifact{}, VerificationReceipt{}, CompletionProvenance{}, err
	}
	receipt := VerificationReceipt{ID: store.ID("evidence"), AttemptID: artifact.AttemptID, ArtifactID: artifact.ID, ActionID: artifact.ActionID, RunID: artifact.RunID, OwnerID: artifact.OwnerID,
		AuthorityContextID: artifact.AuthorityContextID, AuthorityContextDigest: artifact.AuthorityContextDigest,
		VerifierID: DoerFileVerifierID, PolicyVersion: DoerFileVerifierPolicyV1, Claim: "selected-file-verified", MediaType: artifact.MediaType,
		ExpectedDigest: artifact.Digest, ObservedDigest: artifact.Digest, Outcome: Passed, ObservedAt: v.now().UTC()}
	receiptWire, err := json.Marshal(receipt)
	if err != nil {
		return RuntimeArtifact{}, VerificationReceipt{}, CompletionProvenance{}, err
	}
	receipt.EvidenceRef, err = v.store.PutBlob(receiptWire)
	if err != nil {
		return RuntimeArtifact{}, VerificationReceipt{}, CompletionProvenance{}, err
	}
	proof, err := v.AuthorizeCompletion(ctx, artifact, []VerificationReceipt{receipt})
	if err != nil {
		return RuntimeArtifact{}, VerificationReceipt{}, CompletionProvenance{}, err
	}
	proof.selectedFileContract, proof.selectedFileCheck = contractDigest, check
	return artifact, receipt, proof, nil
}

func ValidateReusableDoerProvenance(proof CompletionProvenance, contractDigest string, artifact RuntimeArtifact, receipts []VerificationReceipt) bool {
	return proof.selectedFileCheck != nil && proof.selectedFileContract == contractDigest && len(receipts) == 1 &&
		receipts[0].VerifierID == DoerFileVerifierID && receipts[0].PolicyVersion == DoerFileVerifierPolicyV1 && receipts[0].Claim == "selected-file-verified" &&
		receipts[0].MediaType == "application/json" && artifact.MediaType == "application/json" &&
		receipts[0].ExpectedDigest == artifact.Digest && receipts[0].ObservedDigest == artifact.Digest &&
		ValidateCompletionProvenance(proof, artifact, receipts)
}
