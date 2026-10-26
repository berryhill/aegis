package hermes

import (
	"context"
	"errors"
)

var ErrContributionStopUnavailable = errors.New("contribution role stop unavailable")

// ContributionStopBinding scopes custody to ONE role process, not native source
// commands or source custody. Epoch is the retained role claim, never a PID lease.
type ContributionStopBinding struct {
	SessionID        string `json:"session_id"`
	RuntimeSessionID string `json:"runtime_session_id"`
	AuthorityID      string `json:"authority_id"`
	AuthorityDigest  string `json:"authority_digest"`
	Role             string `json:"role"`
	Epoch            uint64 `json:"epoch"`
	PID              int    `json:"pid"`
	ProcessStart     string `json:"process_start"`
}

// ContributionStopProof cannot be constructed as an acknowledgement by callers.
// Only StopContributionRole can mint the private seal after command.Wait and
// group quiescence. It is not proof that separately owned native effects stopped.
type ContributionStopProof struct {
	binding      ContributionStopBinding
	boot         string
	acknowledged bool
	kind         string
}

func (p ContributionStopProof) Binding() ContributionStopBinding { return p.binding }
func (p ContributionStopProof) Kind() string {
	if p.kind != "" {
		return p.kind
	}
	if p.acknowledged {
		return "owned-wait.v1"
	}
	return ""
}
func (p ContributionStopProof) BootID() string                { return p.boot }
func (p ContributionStopProof) RoleProcessAcknowledged() bool { return p.acknowledged }
func (a *Adapter) StopContributionRole(ctx context.Context, b ContributionStopBinding) (ContributionStopProof, error) {
	if b.SessionID == "" || b.RuntimeSessionID == "" || b.AuthorityID == "" || b.AuthorityDigest == "" || b.Role == "" || b.Epoch == 0 || b.PID <= 0 || b.ProcessStart == "" {
		return ContributionStopProof{}, ErrContributionStopUnavailable
	}
	return a.stopContributionRole(ctx, b)
}
