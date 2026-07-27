package execution

import (
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

func TestValidateAdmissionRequiresFreshExactAuthority(t *testing.T) {
	contract, at := testLaunchContract()
	valid := AdmissionDecision{Allowed: true, AuthorityContextID: contract.AuthorityContext.ID, AuthorityContextDigest: contract.AuthorityContext.Digest, CheckedAt: at}
	if err := ValidateAdmission(contract, valid, at); err != nil {
		t.Fatalf("valid admission rejected: %v", err)
	}
	tests := []struct {
		name string
		edit func(*AdmissionDecision)
	}{
		{"denied", func(d *AdmissionDecision) { d.Allowed = false }},
		{"wrong context", func(d *AdmissionDecision) { d.AuthorityContextID = "other" }},
		{"wrong digest", func(d *AdmissionDecision) { d.AuthorityContextDigest = "sha256:stale" }},
		{"stale", func(d *AdmissionDecision) { d.CheckedAt = at.Add(-2 * time.Second) }},
		{"future", func(d *AdmissionDecision) { d.CheckedAt = at.Add(time.Nanosecond) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := valid
			test.edit(&decision)
			if err := ValidateAdmission(contract, decision, at); err == nil {
				t.Fatal("invalid admission accepted")
			}
		})
	}
}

func TestExecutionTransitionsAreBoundedAndTerminal(t *testing.T) {
	if !CanTransition(StateRequested, StateStarted) || !CanTransition(StateStarted, StateSucceeded) {
		t.Fatal("valid execution transition rejected")
	}
	for _, state := range []State{StateSucceeded, StateFailed, StateDenied, StateCancelled, StateExpired} {
		if CanTransition(state, StateStarted) || CanTransition(state, StateSucceeded) {
			t.Fatalf("terminal state %q allowed another transition", state)
		}
	}
}

func testLaunchContract() (LaunchContract, time.Time) {
	issuedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	at := issuedAt.Add(time.Minute)
	expiresAt := issuedAt.Add(time.Hour)
	runtime := core.RuntimeDescriptor{Runtime: "hermes-agent", Version: "0.18.2"}
	mandate := core.Mandate{ID: "mandate-1", Subject: core.Subject{ID: "subject-1", ExpiresAt: expiresAt}, AgentID: "agent-1", StanzaID: "principal", CharterRevision: 1, CharterDigest: "sha256:charter", Runtime: runtime, Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}, Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}}, IssuedAt: issuedAt, ExpiresAt: expiresAt}
	authority := core.AuthorityContext{ID: "authority-1", MandateID: mandate.ID, SessionID: "session-1", SubjectID: mandate.Subject.ID, AgentID: mandate.AgentID, CharterRevision: 1, CharterDigest: mandate.CharterDigest, Runtime: runtime, Authority: core.EffectiveAuthority{StanzaID: mandate.StanzaID, Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}, Hermes: mandate.Hermes}, IssuedAt: issuedAt, ExpiresAt: expiresAt}
	authority.Digest = core.AuthorityContextDigest(authority)
	startedAt, finishedAt := issuedAt.Add(time.Second), issuedAt.Add(2*time.Second)
	dispatch := Dispatch{ID: "dispatch-1", AuthorityContextID: authority.ID, State: StateSucceeded, RequestedAt: issuedAt, StartedAt: &startedAt, FinishedAt: &finishedAt}
	return LaunchContract{OwnerID: "owner-1", Mandate: mandate, AuthorityContext: authority, ParentDispatch: dispatch}, at
}
