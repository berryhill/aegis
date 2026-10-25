package implementation

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/berryhill/aegis/internal/loop"
)

// InputObservation is a controller-only port. Bytes never appear in history.
// Completion is emitted only after the adapter successfully returns, before
// decoding the untrusted patch (malformed output does not erase delivery).
type InputObservation struct {
	Bytes            []byte
	Completed        bool
	TurnID           string
	RuntimeSessionID string
}

type InputEvidence struct {
	RunID            string `json:"run_id"`
	PassID           string `json:"pass_id"`
	ContractDigest   string `json:"contract_digest"`
	TaskDigest       string `json:"task_digest"`
	ContextDigest    string `json:"context_digest"`
	InputDigest      string `json:"input_digest"`
	InputBytes       int    `json:"input_bytes"`
	Delivered        bool   `json:"delivered"`
	TurnID           string `json:"turn_id,omitempty"`
	RuntimeSessionID string `json:"runtime_session_id,omitempty"`
}

func (e *Executor) inputObserver(r *Record, c loop.VerifiedImplementation, passID string) func(InputObservation) error {
	return func(o InputObservation) error {
		reject := errors.New("managed input observation rejected")
		if len(o.Bytes) == 0 || len(o.Bytes) > 64<<10 || len(r.Passes) == 0 {
			return reject
		}
		p := &r.Passes[len(r.Passes)-1]
		if p.ID != passID {
			return reject
		}
		if !o.Completed {
			if p.Input != nil || o.TurnID != "" || o.RuntimeSessionID != "" {
				return reject
			}
			if err := e.validateInputWire(c, *r, passID, o.Bytes); err != nil {
				return err
			}
			d, err := e.blob(o.Bytes)
			if err != nil {
				return err
			}
			v := InputEvidence{RunID: r.RunID, PassID: passID, ContractDigest: r.ContractDigest, TaskDigest: digest([]byte(c.Task)), ContextDigest: r.ContextDigest, InputDigest: d, InputBytes: len(o.Bytes)}
			p.Input = &v
			return e.stage(r, passID, "input_prepared", v)
		}
		if p.Input == nil || p.Input.Delivered || p.Input.InputDigest != digest(o.Bytes) || o.TurnID != passID {
			return reject
		}
		v := *p.Input
		v.Delivered, v.TurnID, v.RuntimeSessionID = true, o.TurnID, o.RuntimeSessionID
		p.Input = &v
		return e.stage(r, passID, "input_delivered", v)
	}
}

func (e *Executor) validateInputWire(c loop.VerifiedImplementation, r Record, passID string, wire []byte) error {
	reject := errors.New("managed input binding mismatch")
	var envelope struct {
		Instruction string  `json:"instruction"`
		Request     Request `json:"request"`
	}
	if json.Unmarshal(wire, &envelope) != nil || envelope.Instruction == "" || envelope.Request.PassID != passID {
		return reject
	}
	cd, err := envelope.Request.Contract.Digest()
	if err != nil || cd != r.ContractDigest {
		return reject
	}
	if c.RepositoryContext == nil || envelope.Request.RepositoryContext == nil {
		return reject
	}
	initial, err := e.loadBlob(r.ContextDigest)
	if err != nil {
		return reject
	}
	actual, err := json.Marshal(envelope.Request.RepositoryContext)
	if err != nil || !bytes.Equal(initial, actual) {
		return reject
	}
	return nil
}

func (e *Executor) revalidateInputs(c loop.VerifiedImplementation, r Record, final bool) error {
	reject := errors.New("managed input evidence mismatch")
	if e.RequireInputEvidence && !r.ManagedInput {
		return reject
	}
	if !r.ManagedInput {
		return nil
	} // Legacy and synthetic claims stay synthetic.
	for _, p := range r.Passes {
		prepared, delivered := 0, 0
		for _, s := range r.Stages {
			if s.PassID != p.ID || (s.Name != "input_prepared" && s.Name != "input_delivered") {
				continue
			}
			b, err := e.StageOutput(r.RunID, s)
			var v InputEvidence
			if err != nil || json.Unmarshal(b, &v) != nil || p.Input == nil {
				return reject
			}
			want := *p.Input
			if s.Name == "input_prepared" {
				prepared++
				want.Delivered = false
				want.TurnID = ""
				want.RuntimeSessionID = ""
			} else {
				delivered++
			}
			if v != want {
				return reject
			}
		}
		if p.Input == nil {
			if final || p.WorkspaceDigest != "" || prepared != 0 || delivered != 0 {
				return reject
			}
			continue // reserved but no model send was prepared
		}
		v := p.Input
		if prepared != 1 || (v.Delivered && (delivered != 1 || v.TurnID != p.ID)) || (!v.Delivered && (delivered != 0 || v.TurnID != "" || v.RuntimeSessionID != "")) || (final && !v.Delivered) {
			return reject
		}
		if v.RunID != r.RunID || v.PassID != p.ID || v.ContractDigest != r.ContractDigest || v.TaskDigest != digest([]byte(c.Task)) || v.ContextDigest != r.ContextDigest || v.InputBytes < 1 || v.InputBytes > 64<<10 {
			return reject
		}
		b, err := e.loadBlob(v.InputDigest)
		if err != nil || len(b) != v.InputBytes || e.validateInputWire(c, r, p.ID, b) != nil {
			return reject
		}
	}
	return nil
}
