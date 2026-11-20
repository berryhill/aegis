package app

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"time"
)

// Preserve canonical null/empty distinctions as validated data strings, just
// like the existing native setup protocol; do not weaken strict companion JSON.
type agentModelAddReviewWire struct {
	Input        AgentModelAddInput    `json:"input"`
	Expected     reference.RevisionRef `json:"expected"`
	OriginalJSON string                `json:"original_json"`
	ProposedJSON string                `json:"proposed_json"`
	ExpiresAt    time.Time             `json:"expires_at"`
	Receipt      string                `json:"receipt,omitempty"`
}

func (r AgentModelAddReview) MarshalJSON() ([]byte, error) {
	if !protectedCanonicalCharterValid(r.Original) || !protectedCanonicalCharterValid(r.Proposed) {
		return nil, ErrDenied
	}
	original, err := json.Marshal(r.Original)
	if err != nil {
		return nil, err
	}
	proposed, err := json.Marshal(r.Proposed)
	if err != nil {
		return nil, err
	}
	return json.Marshal(agentModelAddReviewWire{r.Input, r.Expected, string(original), string(proposed), r.ExpiresAt, r.Receipt})
}
func (r *AgentModelAddReview) UnmarshalJSON(raw []byte) error {
	var wire agentModelAddReviewWire
	if strictProtectedReviewFact(raw, &wire) != nil {
		return ErrDenied
	}
	var original, proposed core.CanonicalCharter
	if strictProtectedReviewFact([]byte(wire.OriginalJSON), &original) != nil || strictProtectedReviewFact([]byte(wire.ProposedJSON), &proposed) != nil || !protectedCanonicalCharterValid(original) || !protectedCanonicalCharterValid(proposed) {
		return ErrDenied
	}
	*r = AgentModelAddReview{Input: wire.Input, Expected: wire.Expected, Original: original, Proposed: proposed, ExpiresAt: wire.ExpiresAt, Receipt: wire.Receipt}
	return nil
}

// ExactModelAddition reconstructs the permissible delta before the protected
// client asks for approval. Two individually valid digests do not prove that
// the successor adds only the requested lane.
func (r AgentModelAddReview) ExactModelAddition() bool {
	if !protectedCanonicalCharterValid(r.Original) || !protectedCanonicalCharterValid(r.Proposed) {
		return false
	}
	var c core.Charter
	p := r.Proposed.Charter
	if json.Unmarshal(r.Original.Canonical, &c) != nil || c.AgentID != r.Input.AgentID || p.Revision <= c.Revision {
		return false
	}
	selected := -1
	for i, stanza := range c.Stanzas {
		if stanza.Enabled {
			if selected >= 0 {
				return false
			}
			selected = i
		}
	}
	if selected < 0 {
		return false
	}
	if _, err := c.Stanzas[selected].Hermes.ResolveLane(r.Input.Provider, r.Input.Model); err == nil {
		return false
	}
	// An already imported successor may have its own creation provenance.
	c.CreatedAt, c.CreatedBy = p.CreatedAt, p.CreatedBy
	want, _, err := addExactModelLane(c, selected, core.HermesLane{Provider: r.Input.Provider, Model: r.Input.Model}, p.Revision)
	return err == nil && want.Digest == r.Proposed.Digest
}

func (r AgentModelAddReview) DisplayJSON() ([]byte, error) {
	r.Receipt = ""
	type data AgentModelAddReview
	return json.MarshalIndent(data(r), "", "  ")
}

type AgentModelAddResult struct{ Agent FleetAgent }

func (r AgentModelAddResult) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(r.Agent)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		AgentJSON string `json:"agent_json"`
	}{string(raw)})
}
func (r *AgentModelAddResult) UnmarshalJSON(raw []byte) error {
	var wire struct {
		AgentJSON string `json:"agent_json"`
	}
	if strictProtectedReviewFact(raw, &wire) != nil {
		return ErrDenied
	}
	return strictProtectedReviewFact([]byte(wire.AgentJSON), &r.Agent)
}
