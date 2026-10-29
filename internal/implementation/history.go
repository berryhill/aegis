package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/berryhill/aegis/internal/loop"
)

// History is a content-free native custody projection, not a completion receipt.
// ReservedPasses includes proposals which failed before producing check evidence.
type History struct {
	Recovery           Recovery        `json:"recovery"`
	Checks             []RecordedCheck `json:"checks,omitempty"`
	Stages             []Stage         `json:"stages,omitempty"`
	ConsumedBefore     uint16          `json:"consumed_before"`
	CumulativeConsumed uint16          `json:"cumulative_consumed"`
	RemainingBudget    uint16          `json:"remaining_budget"`
	PredecessorRun     string          `json:"predecessor_run,omitempty"`
	PredecessorHistory string          `json:"predecessor_history,omitempty"`
	RunID              string          `json:"run_id"`
	ContractDigest     string          `json:"contract_digest"`
	TaskDigest         string          `json:"task_digest"`
	ContextDigest      string          `json:"context_digest,omitempty"`
	State              string          `json:"state"`
	ReservedPasses     uint16          `json:"reserved_passes"`
	Passes             []Pass          `json:"passes"`
	Digest             string          `json:"digest"`
	AcceptanceGate     string          `json:"acceptance_gate"`
}

type RecordedCheck struct {
	PassID       string `json:"pass_id"`
	Digest       string `json:"digest"`
	OutputDigest string `json:"output_digest"`
	Outcome      string `json:"outcome"`
}

// History never rereads host files: historical evidence remains readable after
// later authorized corrections. It verifies every referenced custody blob.
func (e *Executor) History(id string, c loop.VerifiedImplementation) (out History, err error) {
	missingRecord := false
	defer func() {
		if err != nil && !missingRecord {
			out = History{}
			err = fmt.Errorf("%w: %v", ErrRecoveryEvidenceInvalid, err)
		}
	}()
	r, err := e.Read(id)
	if err != nil {
		missingRecord = errors.Is(err, ErrNotFound)
		return out, err
	}
	cd, err := c.Digest()
	if err != nil {
		return out, err
	}
	reject := errors.New("native coding history binding mismatch")
	if r.RunID != id || r.ContractDigest != cd || len(r.Passes) > int(c.MaxPasses) || uint32(r.ConsumedBefore)+uint32(len(r.Passes)) > uint32(c.MaxPasses) {
		return out, reject
	}
	if r.PredecessorRun != "" {
		// Strictly increasing consumption bounds recursion and rejects cycles.
		previous, x := e.Read(r.PredecessorRun)
		if x != nil || previous.ConsumedBefore >= r.ConsumedBefore || r.ConsumedBefore >= uint16(c.MaxPasses) {
			return out, reject
		}
		h, x := e.History(r.PredecessorRun, c)
		if x != nil || h.State != "failed" || h.Digest != r.PredecessorHistory || h.CumulativeConsumed != r.ConsumedBefore || h.ContextDigest != r.ContextDigest || h.ReservedPasses == 0 {
			return out, reject
		}
		if _, _, x = e.recoveryPreimage(previous, c); x != nil {
			return out, reject
		}
		successor, x := e.DB.Get([]byte("implementation/successor/" + digest([]byte(r.PredecessorRun))))
		if x != nil || string(successor) != id {
			return out, reject
		}
	} else if r.ConsumedBefore != 0 || r.PredecessorHistory != "" {
		return out, reject
	}
	switch r.State {
	case "running", "interrupted", "failed", "succeeded", "needs_input", "denied", "revoked", "expired", "cancelled":
	default:
		return out, reject
	}
	if c.RepositoryContext == nil && r.ContextDigest != "" {
		return out, reject
	}
	if r.ContextDigest != "" {
		b, x := e.loadBlob(r.ContextDigest)
		if x != nil {
			return out, reject
		}
		var initial CollectedRepositoryContext
		if json.Unmarshal(b, &initial) != nil || c.RepositoryContext == nil {
			return out, reject
		}
		identity, x := c.RepositoryContext.Digest()
		if x != nil || initial.Identity != identity || initial.SourceDigest != c.RepositoryContext.SourceDigest || len(initial.Files) != len(c.RepositoryContext.Files) {
			return out, reject
		}
		for i, f := range initial.Files {
			original := c.RepositoryContext.Files[i]
			if f.Path != original.Path || f.Role != original.Role || f.Digest != original.Digest || digest([]byte(f.Text)) != original.Digest || int64(len(f.Text)) != original.Bytes {
				return out, reject
			}
		}
	}
	for i, p := range r.Passes {
		if p.ID != fmt.Sprintf("%s/pass/%d", id, i+1) {
			return out, reject
		}
		// Only the last reservation can lack a completed checker boundary.
		incomplete := p.WorkspaceDigest == "" || p.OutputDigest == ""
		if incomplete {
			if i != len(r.Passes)-1 || p.Passed || p.ContextStateDigest != "" || p.ContextIdentity != "" || p.WorkspaceDigest != "" || p.OutputDigest != "" {
				return out, reject
			}
			continue
		}
		for _, d := range []string{p.WorkspaceDigest, p.OutputDigest} {
			if _, x := e.loadBlob(d); x != nil {
				return out, reject
			}
		}
		if p.Passed {
			output, x := e.loadBlob(p.OutputDigest)
			if x != nil || !requiredTestsPassed(output, c.Policy.RequiredTests) {
				return out, reject
			}
		}
		if c.RepositoryContext != nil {
			identity, x := c.RepositoryContext.Digest()
			if x != nil || p.ContextIdentity != identity || r.ContextDigest == "" {
				return out, reject
			}
			b, x := e.loadBlob(p.ContextStateDigest)
			if x != nil {
				return out, reject
			}
			var state CollectedRepositoryContext
			if json.Unmarshal(b, &state) != nil || state.Identity != identity || state.SourceDigest != c.RepositoryContext.SourceDigest || len(state.Files) != len(c.RepositoryContext.Files) {
				return out, reject
			}
			writable := map[string]bool{}
			for _, name := range c.WritableFiles {
				writable[name] = true
			}
			for n, f := range state.Files {
				original := c.RepositoryContext.Files[n]
				if f.Path != original.Path || f.Role != original.Role || digest([]byte(f.Text)) != f.Digest || (!writable[f.Path] && (f.Digest != original.Digest || int64(len(f.Text)) != original.Bytes)) {
					return out, reject
				}
			}
		} else if p.ContextIdentity != "" || p.ContextStateDigest != "" {
			return out, reject
		}
	}
	var checks []RecordedCheck
	seenStages := map[string]bool{}
	for _, s := range r.Stages {
		switch s.Name {
		case "gate", "input_prepared", "input_delivered", "proposal_rejected", "implement", "judgment", "check", "diagnosis":
		default:
			return out, reject
		}
		key := s.PassID + "/" + s.Name
		if seenStages[key] {
			return out, reject
		}
		seenStages[key] = true
		if s.Name == "gate" {
			if s.PassID != "" {
				return out, reject
			}
		} else {
			found := false
			for _, p := range r.Passes {
				if p.ID == s.PassID {
					found = true
				}
			}
			if !found {
				return out, reject
			}
		}
		b, x := e.StageOutput(id, s)
		if x != nil {
			return out, reject
		}
		if s.Name == "check" {
			var stage struct {
				OutputDigest string `json:"output_digest"`
				Passed       bool   `json:"passed"`
			}
			// Canonical closed payload rejects unknown, duplicate, missing, null and secret fields.
			if json.Unmarshal(b, &stage) != nil {
				return out, reject
			}
			canonical, _ := json.Marshal(stage)
			if !bytes.Equal(b, canonical) {
				return out, reject
			}
			for _, p := range r.Passes {
				if p.ID != s.PassID {
					continue
				}
				output, err := e.loadBlob(stage.OutputDigest)
				if err != nil || p.OutputDigest == "" || stage.OutputDigest != p.OutputDigest || (stage.Passed && !requiredTestsPassed(output, c.Policy.RequiredTests)) || (p.Passed && !stage.Passed) {
					return out, reject
				}
				outcome := "failed"
				if stage.Passed {
					outcome = "passed"
				}
				checks = append(checks, RecordedCheck{PassID: p.ID, Digest: s.Digest, OutputDigest: stage.OutputDigest, Outcome: outcome})
			}
		}
		if s.Name == "proposal_rejected" {
			var f ManagedFailure
			if json.Unmarshal(b, &f) != nil || !IsProposalRejection(f) {
				return out, reject
			}
			for _, p := range r.Passes {
				if p.ID == s.PassID && (p.Failure == nil || *p.Failure != f || p.Input == nil || !p.Input.Delivered) {
					return out, reject
				}
			}
		}
	}
	if err = e.revalidateInputs(c, r, r.State == "succeeded"); err != nil {
		return out, err
	}
	if r.State == "succeeded" && (len(r.Passes) == 0 || !r.Passes[len(r.Passes)-1].Passed) {
		return out, reject
	}
	b, err := json.Marshal(r)
	if err != nil {
		return out, err
	}
	out = History{RunID: id, ContractDigest: cd, TaskDigest: digest([]byte(c.Task)), ContextDigest: r.ContextDigest, State: r.State, ReservedPasses: uint16(len(r.Passes)), Passes: append([]Pass(nil), r.Passes...), Digest: digest(b), AcceptanceGate: "trusted offline go-test.v1; historical presence is not software acceptance"}
	out.Stages = append([]Stage(nil), r.Stages...)
	out.Checks = checks
	out.ConsumedBefore = r.ConsumedBefore
	out.CumulativeConsumed = r.ConsumedBefore + out.ReservedPasses
	out.RemainingBudget = uint16(c.MaxPasses) - out.CumulativeConsumed
	out.PredecessorRun, out.PredecessorHistory = r.PredecessorRun, r.PredecessorHistory
	out.Recovery = e.recovery(r, c)
	return out, nil
}

// CheckRecoveryState is a read-only prerequisite, not authority to continue.
// Partial apply/proposal histories cannot establish the current preimage safely.
func (e *Executor) CheckRecoveryState(id string, c loop.VerifiedImplementation) (History, error) {
	h, err := e.History(id, c)
	if err != nil {
		return h, err
	}
	if h.State != "failed" || h.ReservedPasses == 0 || h.RemainingBudget == 0 || c.RepositoryContext == nil {
		return h, errors.New("native coding recovery requires known failure and remaining budget")
	}
	last := h.Passes[len(h.Passes)-1]
	if last.ContextStateDigest == "" || last.OutputDigest == "" || last.WorkspaceDigest == "" {
		r, x := e.Read(id)
		if x != nil {
			return h, x
		}
		d, _, x := e.recoveryPreimage(r, c)
		if x != nil {
			return h, x
		}
		b, x := e.loadBlob(d)
		var expected CollectedRepositoryContext
		if x != nil || json.Unmarshal(b, &expected) != nil {
			return h, errors.New("native coding preimage unavailable")
		}
		return h, verifyContext(context.Background(), c, &expected)
	}
	r, err := e.Read(id)
	if err != nil {
		return h, err
	}
	if r.ConsumedBefore+h.ReservedPasses >= uint16(c.MaxPasses) {
		return h, errors.New("native coding recovery budget exhausted")
	}
	if err = e.revalidateContext(c, r); err != nil {
		return h, err
	}
	b, err := contractSnapshot(c)
	if err != nil {
		return h, err
	}
	if digest(b) != last.WorkspaceDigest {
		return h, errors.New("native coding recovery workspace drift")
	}
	return h, nil
}
