package api

import (
	"github.com/berryhill/aegis/internal/app"
	consoleweb "github.com/berryhill/aegis/web/console"
)

// Consume only history already validated by the exact Queue/Attempt/Loop reader.
// Never select a previous failed attempt just because it has a useful diagnostic.
// This is recorded acceptance, not another receipt verifier or content reader.
func consoleQueueNativeOutcome(d app.ExecutionDebugger) *consoleweb.QueueNativeOutcomeModel {
	if !d.Terminal || !d.Direct || len(d.Nodes) != 1 || d.DispositionID == "" || d.DispositionAttemptID == "" || d.DispositionExecutionID == "" {
		return nil
	}
	n := d.Nodes[0]
	if n.ExecutionID != d.DispositionExecutionID {
		return nil
	}
	var selected *app.DebuggerAttempt
	for i := range n.Attempts {
		a := &n.Attempts[i]
		if a.ID == d.DispositionAttemptID {
			if selected != nil {
				return nil
			}
			selected = a
		}
	}
	if selected == nil || selected.Coding == nil {
		return nil
	}
	h := selected.Coding
	if h.RunID != d.QueueID || h.Digest == "" || h.ContractDigest == "" || len(h.Passes) == 0 || int(h.ReservedPasses) != len(h.Passes) {
		return nil
	}
	p := h.Passes[len(h.Passes)-1]
	if p.ID == "" {
		return nil
	}
	out := &consoleweb.QueueNativeOutcomeModel{AttemptID: selected.ID, ExecutionID: n.ExecutionID, PassID: p.ID, HistoryDigest: h.Digest, ContractDigest: h.ContractDigest, Consumed: h.CumulativeConsumed, Remaining: h.RemainingBudget}
	for _, s := range h.Stages {
		if s.Name == "check" && s.PassID == p.ID && s.Digest != "" && s.OutputDigest != "" && s.OutputDigest == p.OutputDigest && (s.CheckOutcome == "passed" || s.CheckOutcome == "failed") {
			out.CheckOutcome, out.CheckDigest, out.CheckOutputDigest = s.CheckOutcome, s.Digest, s.OutputDigest
		}
	}
	switch {
	case d.NativeEvidenceVerified && d.State == "succeeded" && d.DispositionReason == "implementation_verified" && h.State == "succeeded" && p.Passed && len(d.ArtifactIDs) == 1 && len(d.ReceiptIDs) == 1:
		out.Accepted = true
		out.ArtifactIDs = append([]string(nil), d.ArtifactIDs...)
		out.ReceiptIDs = append([]string(nil), d.ReceiptIDs...)
	case d.State == "failed" && d.DispositionReason == "implementation_failed" && h.State == "failed" && !p.Passed:
		if p.Failure != nil {
			out.FailureCode = app.ReviewedCodingFailureCode(p.Failure.Code)
			if out.FailureCode == "" {
				out.FailureCode = app.ReviewedCodingFailureCode(p.Failure.Reason)
			}
		}

	default:
		return nil
	}
	return out
}
