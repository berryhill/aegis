package app

import (
	"context"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

// ReviewedCodingFailureCode projects the controller's closed diagnostic vocabulary
// through the application boundary without exposing orchestration to transports.
func ReviewedCodingFailureCode(code string) string {
	return orchestration.ReviewedDoerFailureCode(code)
}

// nativeDebuggerEvidence reloads immutable artifact and receipt custody. History
// alone never establishes acceptance. This does not reread today's host files.
func (s *Service) nativeDebuggerEvidence(ctx context.Context, d ExecutionDebugger) error {
	if len(d.Nodes) != 1 || len(d.ArtifactIDs) != 1 || len(d.ReceiptIDs) != 1 {
		return fleet.ErrCorrupt
	}
	n := d.Nodes[0]
	if n.ExecutionID != d.DispositionExecutionID {
		return fleet.ErrCorrupt
	}
	disposition, err := s.FleetRepository.GetDispositionByGraphRun(ctx, d.GraphRunID)
	if err != nil {
		return err
	}
	a, err := s.FleetRepository.GetRuntimeArtifact(ctx, d.ArtifactIDs[0])
	if err != nil {
		return err
	}
	r, err := s.FleetRepository.GetVerificationReceipt(ctx, d.ReceiptIDs[0])
	if err != nil {
		return err
	}
	revision, err := s.FleetRepository.GetLoopRevision(ctx, n.Loop.ID, n.Loop.Revision)
	if err != nil || revision.Digest != n.Loop.Digest || revision.Doer == nil || revision.Doer.Coding == nil {
		return fleet.ErrCorrupt
	}
	action := false
	for _, step := range revision.Steps {
		if step.ID == a.ActionID && step.Executable != nil && step.Executable.Operation == loop.DoerVerify {
			action = true
		}
	}
	if !action || a.Validate() != nil || r.Validate() != nil || a.ID != d.ArtifactIDs[0] || r.ID != d.ReceiptIDs[0] || a.AttemptID != d.DispositionAttemptID || a.RunID != n.ExecutionID || a.OwnerID != n.Participant.ID || a.AuthorityContextID != disposition.Authority.ID || a.AuthorityContextDigest != disposition.Authority.Digest || a.MediaType != "application/json" {
		return fleet.ErrCorrupt
	}
	if r.AttemptID != a.AttemptID || r.ArtifactID != a.ID || r.ActionID != a.ActionID || r.RunID != a.RunID || r.OwnerID != a.OwnerID || r.AuthorityContextID != a.AuthorityContextID || r.AuthorityContextDigest != a.AuthorityContextDigest || r.Outcome != evidence.Passed || r.VerifierID != "aegis.implementation.verifier" || r.PolicyVersion != loop.VerifiedImplementationSchema || r.Claim != "verified-implementation" || r.MediaType != a.MediaType || r.ExpectedDigest != a.Digest || r.ObservedDigest != a.Digest {
		return fleet.ErrCorrupt
	}
	matched := false
	for _, attempt := range n.Attempts {
		if attempt.ID == d.DispositionAttemptID && attempt.Coding != nil {
			h := attempt.Coding
			if h.State != "succeeded" || len(h.Passes) == 0 {
				return fleet.ErrCorrupt
			}
			p := h.Passes[len(h.Passes)-1]
			if !p.Passed || p.WorkspaceDigest != a.Digest {
				return fleet.ErrCorrupt
			}
			matched = true
		}
	}
	if !matched {
		return fleet.ErrCorrupt
	}
	verifier, err := evidence.NewBlobVerifier(s.Store)
	if err != nil {
		return err
	}
	_, err = verifier.AuthorizeCompletion(ctx, a, []evidence.VerificationReceipt{r})
	return err
}
