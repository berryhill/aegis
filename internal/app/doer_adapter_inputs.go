package app

import (
	"bytes"
	"encoding/json"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"reflect"
	"time"
)

// DoerSetupReviewInput pins the retained draft version. A review grants nothing.
type DoerSetupReviewInput struct {
	ID              string `json:"id"`
	ExpectedVersion uint64 `json:"expected_version"`
}
type DoerSetupReview struct {
	Proposal  DoerSetupProposal `json:"proposal"`
	Receipt   string            `json:"receipt"`
	ExpiresAt time.Time         `json:"expires_at"`
	// Human opens this protected browser surface and reviews a fresh exact receipt.
	ConfirmationURL string `json:"confirmation_url"`
}

// Decision carries no editable proposal: the owning service retains exact bytes.
type DoerSetupDecisionInput struct {
	ID       string `json:"id"`
	Receipt  string `json:"receipt"`
	Decision string `json:"decision"`
}

// DoerReadinessInput explicitly opts into the existing bounded helper probe.
// It carries proposal references only, never authority or approval.
type DoerReadinessInput struct {
	IdempotencyKey   string                  `json:"idempotency_key,omitempty"`
	Activate         bool                    `json:"activate,omitempty"`
	Loop             reference.RevisionRef   `json:"loop,omitempty"`
	Inputs           []graph.NormalizedInput `json:"inputs,omitempty"`
	Agent            reference.RevisionRef   `json:"agent"`
	Candidate        loop.LoopRevision       `json:"candidate"`
	Probe            bool                    `json:"probe,omitempty"`
	runFieldsPresent bool
}

func (i DoerReadinessInput) validateShape() error {
	if i.Loop == (reference.RevisionRef{}) {
		if i.runFieldsPresent || i.Inputs != nil || i.Activate || i.IdempotencyKey != "" || i.Candidate.SchemaVersion != loop.DoerRevisionSchemaVersion {
			return ErrDenied
		}
		return nil
	}
	if i.Loop.Validate() != nil || !reflect.DeepEqual(i.Candidate, loop.LoopRevision{}) || len(i.IdempotencyKey) > 256 {
		return ErrDenied
	}
	return nil
}

// Keep transport presence strict while accepting the zero struct fields emitted
// by existing typed v4/v5 callers. Null is never an omitted request field.
func (i *DoerReadinessInput) UnmarshalJSON(b []byte) error {
	if reference.RejectDuplicateObjectKeys(b) != nil || bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		return ErrDenied
	}
	type wire DoerReadinessInput
	var value wire
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	for key, raw := range fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return ErrDenied
		}
		if key == "inputs" || key == "activate" || key == "idempotency_key" {
			value.runFieldsPresent = true
		}
	}
	*i = DoerReadinessInput(value)
	return i.validateShape()
}

// ContinueDoerDraftInput retains the exact optimistic version and prior Agent
// binding. Charter identifies an already approved successor; it does not approve it.
type ContinueDoerDraftInput struct {
	ID              string                `json:"id"`
	ExpectedVersion uint64                `json:"expected_version"`
	Expected        reference.RevisionRef `json:"expected"`
	Charter         reference.RevisionRef `json:"charter"`
}
