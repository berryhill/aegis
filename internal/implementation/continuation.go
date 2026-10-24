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
	return &Continuation{previous, head, successor, consumed, remaining}, nil
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
	current, err := load(r.Passes[len(r.Passes)-1].ContextStateDigest)
	return initial, current, err
}
