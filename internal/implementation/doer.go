package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Decisions and diagnoses are proposals, never controller authority.
type GateDecision struct {
	Specified     bool `json:"specified"`
	ResultDefined bool `json:"result_defined"`
}
type Judgment struct {
	Done         bool `json:"done"`
	StaysInScope bool `json:"stays_in_scope"`
	Fulfills     bool `json:"fulfills"`
	Works        bool `json:"works"`
	Practices    bool `json:"practices"`
}

func (j Judgment) Passed() bool {
	return j.Done && j.StaysInScope && j.Fulfills && j.Works && j.Practices
}

type Decision interface {
	Gate(context.Context, string) (GateDecision, error)
	Judge(context.Context, string, string) (Judgment, error)
}
type Proposal struct {
	Edits  []Edit `json:"edits"`
	Report string `json:"report"`
}
type ReportingProposer interface {
	ProposeReport(context.Context, Request) (Proposal, error)
}
type DiagnosisRequest struct {
	Task       string
	Acceptance string
	PassID     string
	Report     string
	Judgment   Judgment
	Check      []byte
	Failures   []string
}
type Diagnostician interface {
	Diagnose(context.Context, DiagnosisRequest) ([]byte, error)
}
type CompletionReporter interface {
	Report(context.Context, Record) (string, error)
}

// Stage identifies a content-addressed persisted stage payload, scoped to one run.
type Stage struct {
	PassID string `json:"pass_id,omitempty"`
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

func (e *Executor) stage(r *Record, passID, name string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) > maxBytes {
		return errors.New("stage output exceeded limit")
	}
	d, err := e.blob(b)
	if err != nil {
		return err
	}
	r.Stages = append(r.Stages, Stage{PassID: passID, Name: name, Digest: d})
	return e.save(*r)
}

// StageOutput reads a stage only after checking the exact run's persisted membership and blob digest.
func (e *Executor) StageOutput(id string, s Stage) ([]byte, error) {
	r, err := e.Read(id)
	if err != nil {
		return nil, err
	}
	if r.RunID != id {
		return nil, errors.New("stage run mismatch")
	}
	for _, item := range r.Stages {
		if item == s && s.Digest != "" {
			return e.loadBlob(s.Digest)
		}
	}
	return nil, fmt.Errorf("stage not found for run %q", id)
}
