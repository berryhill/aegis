package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
)

// Private create-only association observed from AttemptTurn.SessionCapability.
// Supervisor runtime and disposable turn runtime are intentionally separate.
type contributionTurnWitness struct {
	Schema                                              string
	Run, Contract, QueueItem, Claim, Attempt, Candidate string
	Role                                                contribution.RoleAuthority
	Turn                                                execution.Turn
}

const contributionTurnWitnessSchema = "aegis.contribution.runtime-turn.v1"

func (s *Service) recordContributionTurn(ctx context.Context, sub core.Subject, c contribution.Contract, claim, attempt string, st contribution.Continuity, role, session string, turn execution.Turn) error {
	a, ok := st.RoleAuthorities[role]
	if !ok || a.SessionID != session || (role != "builder" && role != "tester") || st.Reservations[turn.ID] != "runtime-turn" || st.Candidate == "" {
		return ErrDenied
	}
	checked, err := s.CheckContributionRoleAs(ctx, sub, session, c.Key, role, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: st.Sequence, ExpectedDigest: st.Digest})
	if err != nil || checked != a {
		return ErrDenied
	}
	var original struct {
		Contract string
		Index    int
		Launch   execution.LaunchContract
	}
	if s.Store.Load("contribution-role", a.Authority.ID, &original) != nil || original.Contract != c.Digest || original.Launch.AuthorityContext.ID != a.Authority.ID || original.Launch.AuthorityContext.Digest != a.Authority.Digest || original.Launch.Mandate.ID != a.MandateID || turn.DispatchID != original.Launch.ParentDispatch.ID || turn.AuthorityContextID != a.Authority.ID || turn.RuntimeSessionID == "" || turn.State != execution.StateStarted || turn.StartedAt == nil || turn.FinishedAt != nil || turn.RequestedAt.After(*turn.StartedAt) {
		return ErrDenied
	}
	expectedIndex := 1
	if role == "tester" {
		expectedIndex = 2
	}
	if original.Index != expectedIndex {
		return ErrDenied
	}
	witness := contributionTurnWitness{Schema: contributionTurnWitnessSchema, Run: c.RunID, Contract: c.Digest, QueueItem: c.QueueItemID, Claim: claim, Attempt: attempt, Candidate: st.Candidate, Role: a, Turn: turn}
	wire, err := json.Marshal(witness)
	if err != nil {
		return err
	}
	ref, err := s.Store.PutBlob(wire)
	if err != nil {
		return err
	}
	return s.Store.Create("contribution-turn", turn.ID, reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: turn.ID, Digest: ref})
}

func (s *Service) readContributionTurn(id string) (contributionTurnWitness, error) {
	var out contributionTurnWitness
	var ref reference.DigestRef
	if s.Store.Load("contribution-turn", id, &ref) != nil || ref.Validate() != nil || ref.ID != id {
		return out, ErrDenied
	}
	wire, err := s.Store.GetBlob(ref.Digest)
	if err != nil || contribution.Digest(wire) != ref.Digest || reference.RejectDuplicateObjectKeys(wire) != nil {
		return out, ErrDenied
	}
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF {
		return out, ErrDenied
	}
	canonical, _ := json.Marshal(out)
	if !bytes.Equal(canonical, wire) || out.Schema != contributionTurnWitnessSchema || out.Turn.ID != id {
		return out, ErrDenied
	}
	return out, nil
}

func (s *Service) matchesContributionTurn(c contribution.Contract, st contribution.Continuity, role contribution.RoleAuthority, id orchestration.ContributionRoleIdentity, lineage, attempt, claim string) bool {
	witness, err := s.readContributionTurn(id.TurnID)
	if err != nil || witness.Claim != claim {
		return false
	}
	var original struct {
		Contract string
		Index    int
		Launch   execution.LaunchContract
	}
	if s.Store.Load("contribution-role", role.Authority.ID, &original) != nil || original.Contract != c.Digest || original.Launch.AuthorityContext.ID != role.Authority.ID || original.Launch.AuthorityContext.Digest != role.Authority.Digest || original.Launch.Mandate.ID != role.MandateID || witness.Turn.DispatchID != original.Launch.ParentDispatch.ID {
		return false
	}
	return contributionTurnMatches(witness, c, st, role, id, lineage, attempt)
}
func contributionTurnMatches(w contributionTurnWitness, c contribution.Contract, st contribution.Continuity, a contribution.RoleAuthority, id orchestration.ContributionRoleIdentity, lineage, attempt string) bool {
	return w.Schema == contributionTurnWitnessSchema && w.Run == c.RunID && w.Contract == c.Digest && w.QueueItem == c.QueueItemID && w.Attempt == attempt && w.Claim != "" && w.Candidate == lineage && w.Role == a && w.Turn.ID == id.TurnID && w.Turn.AuthorityContextID == a.Authority.ID && w.Turn.RuntimeSessionID == id.RuntimeSessionID && w.Turn.RuntimeSessionID != "" && w.Turn.State == execution.StateStarted && w.Turn.StartedAt != nil && w.Turn.FinishedAt == nil && st.Reservations[id.TurnID] == "runtime-turn"
}
