package orchestration

import "github.com/berryhill/aegis/internal/implementation"

// CodingContinuation is an opaque controller capability. Only native custody
// validation can produce its kernel state; application callers cannot forge it.
type CodingContinuation struct{ native *implementation.Continuation }

// CodingHistoryView contains content-free, verified custody references only.
// These service DTOs deliberately do not expose native kernel/state types.
type CodingStageView struct {
	PassID string `json:"pass_id,omitempty"`
	Name   string `json:"name"`
	Digest string `json:"digest"`
}
type CodingHistoryView struct {
	Stages             []CodingStageView `json:"stages,omitempty"`
	ConsumedBefore     uint16            `json:"consumed_before"`
	CumulativeConsumed uint16            `json:"cumulative_consumed"`
	RemainingBudget    uint16            `json:"remaining_budget"`
	PredecessorRun     string            `json:"predecessor_run,omitempty"`
	PredecessorHistory string            `json:"predecessor_history,omitempty"`
	RunID              string            `json:"run_id"`
	ContractDigest     string            `json:"contract_digest"`
	TaskDigest         string            `json:"task_digest"`
	ContextDigest      string            `json:"context_digest,omitempty"`
	State              string            `json:"state"`
	ReservedPasses     uint16            `json:"reserved_passes"`
	Passes             []CodingPassView  `json:"passes"`
	Digest             string            `json:"digest"`
	AcceptanceGate     string            `json:"acceptance_gate"`
}
type CodingPassView struct {
	Failure            *CodingFailureView `json:"failure,omitempty"`
	Input              *CodingInputView   `json:"input,omitempty"`
	ContextIdentity    string             `json:"context_identity,omitempty"`
	ContextStateDigest string             `json:"context_state_digest,omitempty"`
	ID                 string             `json:"id"`
	WorkspaceDigest    string             `json:"workspace_digest"`
	OutputDigest       string             `json:"output_digest"`
	Passed             bool               `json:"passed"`
}

type CodingFailureView struct {
	State  string `json:"state,omitempty"`
	Reason string `json:"reason,omitempty"`
	Code   string `json:"code,omitempty"`
}

type CodingInputView struct {
	InputDigest      string `json:"input_digest"`
	InputBytes       int    `json:"input_bytes"`
	ContextDigest    string `json:"context_digest"`
	ContractDigest   string `json:"contract_digest"`
	TaskDigest       string `json:"task_digest"`
	Delivered        bool   `json:"delivered"`
	TurnID           string `json:"turn_id,omitempty"`
	RuntimeSessionID string `json:"runtime_session_id,omitempty"`
}

func codingHistoryView(h implementation.History) CodingHistoryView {
	v := CodingHistoryView{ConsumedBefore: h.ConsumedBefore, CumulativeConsumed: h.CumulativeConsumed, RemainingBudget: h.RemainingBudget, PredecessorRun: h.PredecessorRun, PredecessorHistory: h.PredecessorHistory, RunID: h.RunID, ContractDigest: h.ContractDigest, TaskDigest: h.TaskDigest, ContextDigest: h.ContextDigest, State: h.State, ReservedPasses: h.ReservedPasses, Digest: h.Digest, AcceptanceGate: h.AcceptanceGate}
	for _, s := range h.Stages {
		v.Stages = append(v.Stages, CodingStageView{PassID: s.PassID, Name: s.Name, Digest: s.Digest})
	}
	if h.Passes != nil {
		v.Passes = make([]CodingPassView, len(h.Passes))
	}
	for i, p := range h.Passes {
		v.Passes[i] = CodingPassView{ContextIdentity: p.ContextIdentity, ContextStateDigest: p.ContextStateDigest, ID: p.ID, WorkspaceDigest: p.WorkspaceDigest, OutputDigest: p.OutputDigest, Passed: p.Passed}
		if p.Failure != nil {
			v.Passes[i].Failure = &CodingFailureView{State: p.Failure.State, Reason: p.Failure.Reason, Code: p.Failure.Code}
		}
		if p.Input != nil {
			b := p.Input
			v.Passes[i].Input = &CodingInputView{InputDigest: b.InputDigest, InputBytes: b.InputBytes, ContextDigest: b.ContextDigest, ContractDigest: b.ContractDigest, TaskDigest: b.TaskDigest, Delivered: b.Delivered, TurnID: b.TurnID, RuntimeSessionID: b.RuntimeSessionID}
		}
	}
	return v
}
