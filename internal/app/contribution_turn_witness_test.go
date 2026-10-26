package app

import (
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/orchestration"
	"github.com/berryhill/aegis/internal/reference"
	"testing"
	"time"
)

func TestContributionTurnWitnessSeparatesRuntimeDomains(t *testing.T) {
	now := time.Now()
	c := contribution.Contract{RunID: "run", Digest: "contract", QueueItemID: "queue"}
	a := contribution.RoleAuthority{Role: "builder", Epoch: 1, SessionID: "supervisor-session", RuntimeSessionID: "supervisor-runtime", Authority: reference.DigestRef{ID: "authority"}}
	id := orchestration.ContributionRoleIdentity{TurnID: "turn", RuntimeSessionID: "disposable-runtime"}
	st := contribution.Continuity{Reservations: map[string]string{"turn": "runtime-turn"}}
	w := contributionTurnWitness{Schema: contributionTurnWitnessSchema, Run: c.RunID, Contract: c.Digest, QueueItem: c.QueueItemID, Claim: "claim", Attempt: "attempt", Candidate: "candidate", Role: a, Turn: execution.Turn{ID: "turn", DispatchID: "dispatch", AuthorityContextID: a.Authority.ID, RuntimeSessionID: id.RuntimeSessionID, State: execution.StateStarted, StartedAt: &now}}
	if a.RuntimeSessionID == id.RuntimeSessionID || !contributionTurnMatches(w, c, st, a, id, "candidate", "attempt") {
		t.Fatal("separate actual identity domains cannot join through witness")
	}
	for _, kind := range []string{"run", "role", "epoch", "authority", "turn", "runtime", "candidate", "attempt", "reserved"} {
		bad := w
		switch kind {
		case "run":
			bad.Run = "other"
		case "role":
			bad.Role.Role = "tester"
		case "epoch":
			bad.Role.Epoch++
		case "authority":
			bad.Turn.AuthorityContextID = "other"
		case "turn":
			bad.Turn.ID = "other"
		case "runtime":
			bad.Turn.RuntimeSessionID = "supervisor-runtime"
		case "candidate":
			bad.Candidate = "other"
		case "attempt":
			bad.Attempt = "other"
		case "reserved":
			bad.Turn.State = execution.StateRequested
		}
		if contributionTurnMatches(bad, c, st, a, id, "candidate", "attempt") {
			t.Fatal("substituted witness accepted", kind)
		}
	}
}
