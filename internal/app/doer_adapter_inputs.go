package app

import (
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

// DoerReadinessInput explicitly opts into the existing bounded helper probe.
// It carries proposal references only, never authority or approval.
type DoerReadinessInput struct {
	Agent     reference.RevisionRef `json:"agent"`
	Candidate loop.LoopRevision     `json:"candidate"`
	Probe     bool                  `json:"probe,omitempty"`
}

// ContinueDoerDraftInput retains the exact optimistic version and prior Agent
// binding. Charter identifies an already approved successor; it does not approve it.
type ContinueDoerDraftInput struct {
	ID              string                `json:"id"`
	ExpectedVersion uint64                `json:"expected_version"`
	Expected        reference.RevisionRef `json:"expected"`
	Charter         reference.RevisionRef `json:"charter"`
}
