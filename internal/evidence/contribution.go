package evidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/berryhill/aegis/internal/reference"
)

const (
	ContributionManifestV1 = "aegis.contribution.manifest.v1"
	ContributionPolicyV1   = "aegis.contribution.verification.v1"
	ContributionReceiptV1  = "aegis.contribution.receipt.v1"
)

var ErrContributionEvidence = errors.New("contribution evidence invalid")

// ContributionBinding pins causal identity, not authority. All references must
// be resolved from the owning services before verification; decoding this value
// cannot authenticate a caller or mint a receipt.
type ContributionBinding struct {
	Run        string                `json:"run"`
	QueueItem  reference.DigestRef   `json:"queue_item"`
	Attempt    string                `json:"attempt"`
	Claim      string                `json:"claim"`
	Invocation reference.DigestRef   `json:"invocation"`
	Contract   reference.DigestRef   `json:"contract"`
	Control    reference.DigestRef   `json:"control"`
	Loop       reference.RevisionRef `json:"loop"`
	Profile    string                `json:"profile"`
	Policy     string                `json:"policy"`
	Candidate  string                `json:"candidate"`
}

// ContributionManifest contains references to immutable producer evidence, not
// copies of their domains or mutable phase statuses. In particular, a digest of
// this manifest is not proof that any referenced behavior or effect occurred.
type ContributionManifest struct {
	Schema            string              `json:"schema"`
	Binding           ContributionBinding `json:"binding"`
	Target            string              `json:"target"`
	CandidateEvidence string              `json:"candidate_evidence"`
	AuthorityEvidence string              `json:"authority_evidence"`
	DeliveryEvidence  []string            `json:"delivery_evidence"`
	IssueEvidence     string              `json:"issue_evidence"`
	ObservedAt        time.Time           `json:"observed_at"`
}

func (b ContributionBinding) Validate() error {
	if b.Run == "" || b.Attempt == "" || b.Claim == "" || b.Policy != ContributionPolicyV1 ||
		!validDigest(b.Profile) || !validDigest(b.Candidate) || b.Loop.Validate() != nil {
		return ErrContributionEvidence
	}
	for _, ref := range []reference.DigestRef{b.QueueItem, b.Invocation, b.Contract, b.Control} {
		if ref.Validate() != nil {
			return ErrContributionEvidence
		}
	}
	return nil
}

func (m ContributionManifest) Validate() error {
	if m.Schema != ContributionManifestV1 || m.Binding.Validate() != nil || m.ObservedAt.IsZero() ||
		!validDigest(m.CandidateEvidence) || !validDigest(m.AuthorityEvidence) || m.DeliveryEvidence == nil {
		return ErrContributionEvidence
	}
	seen := map[string]bool{m.CandidateEvidence: true, m.AuthorityEvidence: true}
	if m.CandidateEvidence == m.AuthorityEvidence || len(m.DeliveryEvidence) > 512 {
		return ErrContributionEvidence
	}
	for _, ref := range m.DeliveryEvidence {
		if !validDigest(ref) || seen[ref] {
			return ErrContributionEvidence
		}
		seen[ref] = true
	}
	switch m.Target {
	case "local-only", "no-publish":
		if len(m.DeliveryEvidence) != 0 || m.IssueEvidence != "" {
			return ErrContributionEvidence
		}
	case "no-merge":
		if len(m.DeliveryEvidence) == 0 || m.IssueEvidence != "" {
			return ErrContributionEvidence
		}
	case "verified-protected-base-merge":
		if len(m.DeliveryEvidence) == 0 || !validDigest(m.IssueEvidence) || seen[m.IssueEvidence] {
			return ErrContributionEvidence
		}
	default:
		return ErrContributionEvidence
	}
	return nil
}

func MarshalContributionManifest(m ContributionManifest) ([]byte, error) {
	if m.Validate() != nil {
		return nil, ErrContributionEvidence
	}
	return json.Marshal(m)
}

// DecodeContributionManifest rejects duplicates, nulls, case aliases, omissions,
// unknown fields, noncanonical encodings and trailing data at every depth.
func DecodeContributionManifest(ref string, content []byte) (ContributionManifest, error) {
	var m ContributionManifest
	if !validDigest(ref) || sha256Reference(content) != ref || strictContributionJSON(content, &m) != nil || m.Validate() != nil {
		return ContributionManifest{}, ErrContributionEvidence
	}
	return m, nil
}

func strictContributionJSON(content []byte, out any) error {
	if len(content) == 0 || len(content) > 4<<20 || reference.RejectDuplicateObjectKeys(content) != nil {
		return ErrContributionEvidence
	}
	// The byte-equality check also rejects omitted scalar fields that Go would
	// otherwise silently initialize, including null-to-zero coercion.
	d := json.NewDecoder(bytes.NewReader(content))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return ErrContributionEvidence
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, content) {
		return ErrContributionEvidence
	}
	var tree any
	if json.Unmarshal(content, &tree) != nil || contributionNull(tree) {
		return ErrContributionEvidence
	}
	return nil
}
func contributionNull(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		for _, item := range x {
			if contributionNull(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range x {
			if contributionNull(item) {
				return true
			}
		}
	}
	return false
}

// ContributionReceipt is a reference-only verification report. Only the joined
// controller verifier may publish one as an authoritative result; this schema's
// structural validity never substitutes for producer provenance or disposition.
type ContributionReceipt struct {
	Schema     string              `json:"schema"`
	Binding    ContributionBinding `json:"binding"`
	Manifest   string              `json:"manifest"`
	Boundary   string              `json:"boundary"`
	Outcome    string              `json:"outcome"`
	Reason     string              `json:"reason"`
	Merge      string              `json:"merge"`
	Evidence   []string            `json:"evidence"`
	ObservedAt time.Time           `json:"observed_at"`
}

func (r ContributionReceipt) Validate() error {
	if r.Schema != ContributionReceiptV1 || r.Binding.Validate() != nil || !validDigest(r.Manifest) || r.Evidence == nil || len(r.Evidence) > 512 || r.ObservedAt.IsZero() {
		return ErrContributionEvidence
	}
	switch r.Boundary {
	case "local-only", "no-publish", "no-merge", "verified-protected-base-merge":
	default:
		return ErrContributionEvidence
	}
	seen := map[string]bool{}
	for _, ref := range r.Evidence {
		if !validDigest(ref) || seen[ref] {
			return ErrContributionEvidence
		}
		seen[ref] = true
	}
	if r.Merge != "" && !contributionGitOID(r.Merge) {
		return ErrContributionEvidence
	}
	switch r.Outcome {
	case "succeeded":
		if r.Boundary != "verified-protected-base-merge" || r.Merge == "" || r.Reason != "contribution_verified" {
			return ErrContributionEvidence
		}
	case "narrower-target-completed":
		if r.Boundary == "verified-protected-base-merge" || r.Merge != "" || r.Reason != "narrower_target_verified" {
			return ErrContributionEvidence
		}
	case "merged-unresolved":
		if r.Merge == "" || r.Reason != "merged_validation_unresolved" {
			return ErrContributionEvidence
		}
	case "failed", "denied", "cancelled", "expired", "exhausted", "unknown":
		// A confirmed irreversible merge cannot disappear into ordinary failure.
		if r.Merge != "" || !contributionFailureReason(r.Outcome, r.Reason) {
			return ErrContributionEvidence
		}
	default:
		return ErrContributionEvidence
	}
	return nil
}
func contributionFailureReason(outcome, reason string) bool {
	switch outcome {
	case "failed":
		return reason == "evidence_invalid" || reason == "execution_failed"
	case "denied":
		return reason == "authority_denied"
	case "cancelled":
		return reason == "run_cancelled"
	case "expired":
		return reason == "authority_expired"
	case "exhausted":
		return reason == "budget_exhausted"
	case "unknown":
		return reason == "effect_unknown"
	}
	return false
}
func contributionGitOID(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func MarshalContributionReceipt(r ContributionReceipt) ([]byte, error) {
	if r.Validate() != nil {
		return nil, ErrContributionEvidence
	}
	return json.Marshal(r)
}
func DecodeContributionReceipt(ref string, content []byte) (ContributionReceipt, error) {
	var r ContributionReceipt
	if !validDigest(ref) || sha256Reference(content) != ref || strictContributionJSON(content, &r) != nil || r.Validate() != nil {
		return ContributionReceipt{}, ErrContributionEvidence
	}
	return r, nil
}
