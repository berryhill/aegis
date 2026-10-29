package implementation

import (
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/loop"
)

// Continuation is controller-derived, opaque to wire/model input. Preparing it
// grants no execution authority: Run repeats admission and custody validation.
type Continuation struct {
	predecessor, history, successor string
	preimage, proof                 string
	consumed, remaining             uint16
}

// PrepareContinuation binds an exact known failed custody head to one successor.
// The caller must independently authorize the successor and remaining cap.
func (e *Executor) PrepareContinuation(previous, head, successor string, remaining uint16, c loop.VerifiedImplementation) (*Continuation, error) {
	h, err := e.CheckRecoveryState(previous, c)
	if err != nil {
		return nil, err
	}
	r, err := e.Read(previous)
	if err != nil {
		return nil, err
	}
	consumed := r.ConsumedBefore + h.ReservedPasses
	if h.Digest != head || previous == successor || successor == "" || consumed >= uint16(c.MaxPasses) || remaining == 0 || remaining != uint16(c.MaxPasses)-consumed {
		return nil, errors.New("native continuation binding or cumulative budget mismatch")
	}
	preimage, proof, err := e.recoveryPreimage(r, c)
	if err != nil {
		return nil, err
	}
	return &Continuation{predecessor: previous, history: head, successor: successor, preimage: preimage, proof: proof, consumed: consumed, remaining: remaining}, nil
}

func (e *Executor) continuationContext(id string, c loop.VerifiedImplementation) (*CollectedRepositoryContext, *CollectedRepositoryContext, error) {
	t := e.Continuation
	verified, err := e.PrepareContinuation(t.predecessor, t.history, id, t.remaining, c)
	if err != nil || *verified != *t {
		return nil, nil, errors.New("native continuation stale or mismatched")
	}
	r, err := e.Read(t.predecessor)
	if err != nil {
		return nil, nil, err
	}
	load := func(d string) (*CollectedRepositoryContext, error) {
		b, err := e.loadBlob(d)
		if err != nil {
			return nil, err
		}
		var v CollectedRepositoryContext
		err = json.Unmarshal(b, &v)
		return &v, err
	}
	initial, err := load(r.ContextDigest)
	if err != nil {
		return nil, nil, err
	}
	current, err := load(t.preimage)
	return initial, current, err
}

// The durable implement report precedes every apply in managed doer.v1.
// A delivered reservation additionally needs its durable closed decode rejection.
// Missing historical classifications are never reconstructed.
func (e *Executor) recoveryPreimage(r Record, c loop.VerifiedImplementation) (string, string, error) {
	reject := errors.New("native coding no-apply witness unavailable")
	if r.State != "failed" || len(r.Passes) == 0 {
		return "", "", reject
	}
	last := r.Passes[len(r.Passes)-1]
	if last.ContextStateDigest != "" && last.WorkspaceDigest != "" && last.OutputDigest != "" {
		return last.ContextStateDigest, "completed-check.v1", nil
	}
	if !r.ManagedInput || c.DecisionMode != "doer.v1" || c.RepositoryContext == nil || r.ContextDigest == "" || last.Input == nil || (last.Input.Delivered && (last.Failure == nil || !IsProposalRejection(*last.Failure))) || last.WorkspaceDigest != "" || last.OutputDigest != "" || last.ContextStateDigest != "" || last.ContextIdentity != "" || last.Passed {
		return "", "", reject
	}
	if e.revalidateInputs(c, r, false) != nil {
		return "", "", reject
	}
	if RepositorySourceDigest(c.RepositoryContext) != c.RepositoryContext.SourceDigest {
		return "", "", reject
	}
	if f := last.Failure; f != nil {
		if f.State != "failed" || f.Reason == "turn_cancelled_or_timed_out" || f.Reason == "prompt_rejected" || f.Reason == "local_inference_release_denied" || f.Reason == "authority_expired" || f.Reason == "authority_no_longer_effective" {
			return "", "", reject
		}
	}
	gate := false
	rejections := 0
	for _, s := range r.Stages {
		if _, err := e.StageOutput(r.RunID, s); err != nil {
			return "", "", reject
		}
		switch s.Name {
		case "gate":
			b, err := e.StageOutput(r.RunID, s)
			var g GateDecision
			if gate || s.PassID != "" || err != nil || json.Unmarshal(b, &g) != nil || !g.Specified || !g.ResultDefined {
				return "", "", reject
			}
			gate = true
		case "proposal_rejected":
			if s.PassID == last.ID {
				b, x := e.StageOutput(r.RunID, s)
				var f ManagedFailure
				if x != nil || json.Unmarshal(b, &f) != nil || last.Failure == nil || f != *last.Failure || !IsProposalRejection(f) {
					return "", "", reject
				}
				rejections++
			}
		case "input_prepared", "input_delivered", "implement", "judgment", "check", "diagnosis":
		default:
			return "", "", reject
		}
		if s.PassID == last.ID && s.Name != "input_prepared" && !(last.Input.Delivered && (s.Name == "input_delivered" || s.Name == "proposal_rejected")) {
			return "", "", reject
		}
	}
	if !gate || (last.Input.Delivered && rejections != 1) {
		return "", "", reject
	}
	preimage := r.ContextDigest
	if len(r.Passes) > 1 {
		preimage = r.Passes[len(r.Passes)-2].ContextStateDigest
	} else if r.PredecessorRun != "" {
		// Do not confuse original immutable model context with a predecessor's candidate.
		return "", "", reject
	}
	if preimage == "" {
		return "", "", reject
	}
	return preimage, "prepared-no-implement.v1", nil
}
