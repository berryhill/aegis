package app

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// ApproveAgentCharterInput approves only this exact immutable successor binding.
// It neither issues authority nor changes runtime, ownership or lifecycle.
type ApproveAgentCharterInput struct {
	Expected reference.RevisionRef `json:"expected"`
	Charter  reference.RevisionRef `json:"charter"`
}

// UnmarshalJSON keeps exact approvals strict across CLI and HTTP transports.
func (input *ApproveAgentCharterInput) UnmarshalJSON(data []byte) error {
	if err := reference.RejectDuplicateObjectKeys(data); err != nil {
		return err
	}
	type wire ApproveAgentCharterInput
	var value wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*input = ApproveAgentCharterInput(value)
	return nil
}

func agentRevisionRef(r registry.AgentRevision) reference.RevisionRef {
	return reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: r.AgentID, Revision: r.Revision, Digest: r.Digest}
}

func (s *Service) validAgentCharterSuccessor(previous, next registry.AgentRevision) bool {
	if next.AgentID != previous.AgentID || next.Revision != previous.Revision+1 || next.Source != previous.Source || next.Runtime != previous.Runtime || next.Ownership != previous.Ownership || previous.Lifecycle == registry.LifecycleRetired || next.Lifecycle != previous.Lifecycle || !reflect.DeepEqual(next.CapabilityDeclarations, previous.CapabilityDeclarations) || !reflect.DeepEqual(next.PolicyRefs, previous.PolicyRefs) {
		return false
	}
	if next.CharterSuccessor == nil || next.CharterSuccessor.Previous != agentRevisionRef(previous) || next.CharterSuccessor.ApprovedBy != previous.Ownership.OwnerID || next.Charter.ID != previous.Charter.ID || next.Charter.Revision <= previous.Charter.Revision {
		return false
	}
	c, err := s.Store.GetCharter(next.Charter.ID, next.Charter.Revision)
	if err != nil || c.Digest != next.Charter.Digest {
		return false
	}
	canonical, err := core.Canonicalize(c.Charter)
	return err == nil && canonical.Digest == c.Digest && c.Charter.AgentID == next.AgentID && c.Charter.Runtime.Adapter == next.Runtime.Adapter && c.Charter.Runtime.Runtime == next.Runtime.Runtime && c.Charter.Runtime.Target == next.Runtime.Target
}

func (s *Service) ApproveAgentCharterAs(ctx context.Context, subject core.Subject, agentID string, input ApproveAgentCharterInput) (FleetAgent, error) {
	if err := s.requireFleetPrincipal(subject); err != nil {
		return FleetAgent{}, err
	}
	if agentID == registry.BuiltInAegisAgentID {
		return FleetAgent{}, ErrDenied
	}
	if input.Expected.Validate() != nil || input.Charter.Validate() != nil || input.Expected.ID != agentID || input.Charter.ID != agentID {
		return FleetAgent{}, ErrConflict
	}
	current, err := s.GetFleetAgentAs(ctx, subject, agentID, 0)
	if err != nil {
		return FleetAgent{}, err
	}
	if current.Revision.Ownership.OwnerID != subject.PrincipalID {
		return FleetAgent{}, ErrDenied
	}
	if agentRevisionRef(current.Revision) != input.Expected {
		return FleetAgent{}, ErrConflict
	}
	next := current.Revision
	next.Revision++
	next.Charter = input.Charter
	next.CharterSuccessor = &registry.CharterSuccessor{Previous: input.Expected, ApprovedBy: subject.PrincipalID}
	if !s.validAgentCharterSuccessor(current.Revision, next) {
		return FleetAgent{}, ErrConflict
	}
	next, err = registry.SealRevision(next)
	if err != nil {
		return FleetAgent{}, err
	}
	err = s.FleetRepository.PublishAgentRevision(ctx, next, fleet.AuditFact{Event: core.AuditEvent{Type: "fleet.agent.charter.approved", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, AgentID: agentID, Outcome: "ok", Reason: "principal approved exact charter successor; no authority issued"}})
	if err != nil {
		return FleetAgent{}, err
	}
	return s.GetFleetAgentAs(ctx, subject, agentID, next.Revision)
}
