package app

import (
	"fmt"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/reference"
)

// ValidateContributionIssueURL keeps canonical issue parsing in the application
// boundary. Validation alone grants no invocation or effect authority.
func ValidateContributionIssueURL(raw string) error {
	_, err := contribution.ParseIssueURL(raw)
	return err
}

// NewContributionLoopReference builds the same exact immutable reference used by
// owning-service admission, without making HTTP adapters depend on domain types.
func NewContributionLoopReference(id string, revision uint64, digest string) (ContributionLoopReference, error) {
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: id, Revision: revision, Digest: digest}
	return ref, ref.Validate()
}

// ContributionPreparationDecisionView is an allowlisted transport DTO, not the
// retained decision or an approval. It carries no raw plans, sessions or secrets.
type ContributionPreparationDecisionView struct {
	Stage, Role, Agent, Charter, PlanID, PlanDigest string
	Candidate, ContractDigest, Status               string
	ExpiresAt                                       time.Time
}

func (s ContributionEngineeringStatus) DecisionView() *ContributionPreparationDecisionView {
	if s.Decision == nil {
		return nil
	}
	d := s.Decision
	return &ContributionPreparationDecisionView{
		Stage: d.Stage, Role: d.Role,
		Agent:   fmt.Sprintf("%s revision %d · %s", d.Binding.Agent.ID, d.Binding.Agent.Revision, d.Binding.Agent.Digest),
		Charter: fmt.Sprintf("%s revision %d · %s", d.Binding.Charter.ID, d.Binding.Charter.Revision, d.Binding.Charter.Digest),
		PlanID:  d.PlanID, PlanDigest: d.PlanDigest, Candidate: d.Candidate,
		ContractDigest: d.ContractDigest, Status: d.Status, ExpiresAt: d.ExpiresAt,
	}
}
