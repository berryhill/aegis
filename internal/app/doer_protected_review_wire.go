package app

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

// Canonical facts legitimately distinguish null and empty collections. Carry
// their complete JSON as explicit data strings on the protected review wire;
// never change canonical bytes/digests or relax the companion's null/unknown-key
// policy. These are validated facts, not authority inputs or executable content.
type doerProtectedReviewData DoerProtectedSetupReview
type doerProtectedReviewWire struct {
	doerProtectedReviewData
	AgentJSON   string `json:"agent_json,omitempty"`
	CharterJSON string `json:"charter_json,omitempty"`
}

func strictProtectedReviewFact(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > DoerDraftMaxBytes || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || reference.RejectDuplicateObjectKeys(raw) != nil {
		return ErrDenied
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return ErrDenied
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return ErrDenied
	}
	return nil
}

func protectedCanonicalCharterValid(c core.CanonicalCharter) bool {
	canonical, err := core.Canonicalize(c.Charter)
	if err != nil || canonical.Digest != c.Digest {
		return false
	}
	var compact bytes.Buffer
	return json.Compact(&compact, c.Canonical) == nil && bytes.Equal(compact.Bytes(), canonical.Canonical)
}

func protectedReviewMetadata(r *core.Review) *core.Review {
	if r == nil {
		return nil
	}
	copy := *r
	if copy.Warnings == nil {
		copy.Warnings = []string{}
	}
	normalize := func(old map[string][]string) map[string][]string {
		result := map[string][]string{}
		for key, value := range old {
			result[key] = append([]string{}, value...)
		}
		return result
	}
	// These are unsealed display maps, not the canonical Plan or charter facts.
	copy.RequestedToolsets = normalize(r.RequestedToolsets)
	copy.CredentialScopes = normalize(r.CredentialScopes)
	copy.MemoryScopes = normalize(r.MemoryScopes)
	copy.ApprovalRequirements = map[string]core.ApprovalPolicy{}
	for key, value := range r.ApprovalRequirements {
		value.RequiredOperations = append([]string{}, value.RequiredOperations...)
		copy.ApprovalRequirements[key] = value
	}
	return &copy
}

func (r DoerProtectedSetupReview) MarshalJSON() ([]byte, error) {
	wire := doerProtectedReviewWire{doerProtectedReviewData: doerProtectedReviewData(r)}
	wire.Agent = nil
	wire.Charter = nil
	wire.ProvisionReview = protectedReviewMetadata(r.ProvisionReview)
	if r.Agent != nil {
		raw, err := registry.MarshalAgentRevision(*r.Agent)
		if err != nil || len(raw) > DoerDraftMaxBytes {
			return nil, ErrDenied
		}
		wire.AgentJSON = string(raw)
	}
	if r.Charter != nil {
		if !protectedCanonicalCharterValid(*r.Charter) {
			return nil, ErrDenied
		}
		raw, err := json.Marshal(r.Charter)
		if err != nil || len(raw) > DoerDraftMaxBytes {
			return nil, ErrDenied
		}
		wire.CharterJSON = string(raw)
	}
	return json.Marshal(wire)
}

func (r *DoerProtectedSetupReview) UnmarshalJSON(raw []byte) error {
	var wire doerProtectedReviewWire
	if strictProtectedReviewFact(raw, &wire) != nil {
		return ErrDenied
	}
	if wire.AgentJSON != "" {
		if wire.Agent != nil {
			return ErrDenied
		}
		agent, err := registry.UnmarshalAgentRevision([]byte(wire.AgentJSON))
		if err != nil {
			return ErrDenied
		}
		wire.Agent = &agent
	}
	if wire.CharterJSON != "" {
		if wire.Charter != nil {
			return ErrDenied
		}
		var charter core.CanonicalCharter
		if strictProtectedReviewFact([]byte(wire.CharterJSON), &charter) != nil || !protectedCanonicalCharterValid(charter) {
			return ErrDenied
		}
		wire.Charter = &charter
	}
	*r = DoerProtectedSetupReview(wire.doerProtectedReviewData)
	return nil
}

// DisplayJSON preserves complete readable canonical facts for the independent
// human. The private one-use receipt is never rendered. Encoding failure means
// unavailable review, not a partial approval display.
func (r DoerProtectedSetupReview) DisplayJSON() ([]byte, error) {
	r.Receipt = ""
	return json.MarshalIndent(doerProtectedReviewData(r), "", "  ")
}
