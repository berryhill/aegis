package orchestration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
)

// ContributionCandidateEvidence is private-custody diagnostic readback. The
// content reference addresses actual native acceptance, not final contribution
// success, role authority, a source lease or permission to publish.
type ContributionCandidateEvidence struct {
	RunID       string `json:"run_id"`
	Contract    string `json:"contract"`
	Attempt     string `json:"attempt"`
	Candidate   string `json:"candidate"`
	EvidenceRef string `json:"evidence_ref"`
}

// ReloadContributionCandidateEvidence independently asks the real native owner
// to reconstruct its original immutable checks, candidate, review and policy.
// It does not accept a policy, role proof, PASS report or reload callback from
// the caller. The immutable Loop and sealed intake select all expected values.
func (w *QueueWorker) ReloadContributionCandidateEvidence(ctx context.Context, subject core.Subject, queueID string, revision loop.LoopRevision) (out ContributionCandidateEvidence, err error) {
	if w == nil || w.blobs == nil {
		return ContributionCandidateEvidence{}, ErrWorkerDenied
	}
	_, err = w.acceptedContributionCandidate(ctx, subject, queueID, revision, func(e *implementation.ManagedContribution, run, contract string, p loop.ContributionExecutionPolicy) (implementation.ContributionCandidate, error) {
		r, x := e.Read(run)
		if x != nil {
			return implementation.ContributionCandidate{}, x
		}
		if r.State != "candidate_accepted" || len(r.Passes) == 0 {
			return implementation.ContributionCandidate{}, ErrWorkerDenied
		}
		v := r.Passes[len(r.Passes)-1].Candidate
		wire, x := e.ReloadAcceptedEvidence(ctx, run, contract, r.QueueAttempt, v.Digest, p)
		if x != nil {
			return implementation.ContributionCandidate{}, x
		}
		sum := sha256.Sum256(wire)
		expected := "sha256:" + hex.EncodeToString(sum[:])
		ref, x := w.blobs.PutBlob(wire)
		if x != nil {
			return implementation.ContributionCandidate{}, x
		}
		loaded, x := w.blobs.GetBlob(ref)
		if x != nil || ref != expected || !bytes.Equal(loaded, wire) {
			return implementation.ContributionCandidate{}, ErrWorkerDenied
		}
		// Repeat actual owner admission and immutable reload after blob storage.
		after, x := e.ReloadAcceptedEvidence(ctx, run, contract, r.QueueAttempt, v.Digest, p)
		if x != nil || !bytes.Equal(after, wire) {
			return implementation.ContributionCandidate{}, ErrWorkerDenied
		}
		out = ContributionCandidateEvidence{run, contract, r.QueueAttempt, v.Digest, ref}
		return v, nil
	})
	if err != nil {
		return ContributionCandidateEvidence{}, err
	}
	return out, nil
}
