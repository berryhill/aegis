package app

import (
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/registry"
)

// DoerProtectedSetupReview is review data, not authority. Receipt is an opaque,
// session-bound one-use handle kept only in the native adapter's memory.
type DoerProtectedSetupReview struct {
	Action          string                 `json:"action"`
	Draft           DoerDraft              `json:"draft"`
	Proposal        *DoerSetupProposal     `json:"proposal,omitempty"`
	ProvisionReview *core.Review           `json:"provision_review,omitempty"`
	Plan            *core.Plan             `json:"plan,omitempty"`
	Charter         *core.CanonicalCharter `json:"charter,omitempty"`
	Host            *DoerHostApprovalInput `json:"host,omitempty"`
	Receipt         string                 `json:"receipt,omitempty"`
}

type DoerProtectedSetupInput struct {
	ID              string `json:"id"`
	ExpectedVersion uint64 `json:"expected_version"`
	Action          string `json:"action"`
}

type DoerProtectedSetupDecision struct {
	Receipt  string `json:"receipt"`
	Decision string `json:"decision"`
}

type DoerProtectedSetupResult struct {
	Successor    *registry.AgentRevision `json:"successor,omitempty"`
	Status       string                  `json:"status"`
	Draft        DoerDraft               `json:"draft"`
	Provisioning *core.Receipt           `json:"provisioning,omitempty"`
	Host         *DoerHostApproval       `json:"host,omitempty"`
}
