package app

import (
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
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
