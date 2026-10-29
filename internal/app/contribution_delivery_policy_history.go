package app

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/delivery"
	"github.com/berryhill/aegis/internal/store"
)

// The original policy has no version field. Retain its exact existing encoding;
// adding an envelope or schema field would change already sealed policy digests.
const contributionDeliveryPolicyMaxBytes = 1 << 20

var contributionDeliveryPolicyDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ContributionDeliveryPolicyHistory reads only the Service's canonical immutable
// blob owner. There is no mutable index, current-policy fallback or public writer.
type ContributionDeliveryPolicyHistory struct{ blobs *store.Store }

var _ ContributionDeliveryHistoricalPolicies = (*ContributionDeliveryPolicyHistory)(nil)

func NewContributionDeliveryPolicyHistory(s *Service) (*ContributionDeliveryPolicyHistory, error) {
	if s == nil || s.Store == nil {
		return nil, ErrContributionDeliveryUnavailable
	}
	return &ContributionDeliveryPolicyHistory{blobs: s.Store}, nil
}

func decodeContributionDeliveryPolicy(wire []byte, digest string) (ContributionDeliveryPolicy, error) {
	var p ContributionDeliveryPolicy
	if !contributionDeliveryPolicyDigest.MatchString(digest) || len(wire) == 0 || len(wire) > contributionDeliveryPolicyMaxBytes || contribution.Digest(wire) != digest {
		return p, delivery.ErrDenied
	}
	d := json.NewDecoder(bytes.NewReader(wire))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return ContributionDeliveryPolicy{}, delivery.ErrDenied
	}
	// Exact re-encoding rejects duplicates (at every depth), trailing values,
	// unknown/case-folded keys, null objects/scalars and noncanonical spellings.
	// Original nil slices remain null: changing those would retcon the old digest.
	canonical, err := json.Marshal(p)
	if err != nil || !bytes.Equal(canonical, wire) || p.ControllerSession == "" || p.Binding.Run == "" || p.Binding.OwnerID == "" || p.Binding.QueueItemID == "" || !contribution.ValidKey(p.Binding.Key) || p.Binding.FenceEpoch == 0 || p.Binding.Base == "" || !contributionDeliveryPolicyDigest.MatchString(p.Binding.RoleBinding) || !contributionDeliveryPolicyDigest.MatchString(p.Binding.Contract) || !contributionDeliveryPolicyDigest.MatchString(p.Binding.Candidate) || p.Loop.Validate() != nil {
		return ContributionDeliveryPolicy{}, delivery.ErrDenied
	}
	return p.snapshot(), nil
}

func (h *ContributionDeliveryPolicyHistory) ResolveContributionDeliveryPolicyDigest(ctx context.Context, digest string) (ContributionDeliveryPolicy, error) {
	if ctx == nil || ctx.Err() != nil || h == nil || h.blobs == nil || !contributionDeliveryPolicyDigest.MatchString(digest) {
		return ContributionDeliveryPolicy{}, delivery.ErrDenied
	}
	// contribution.Digest and the existing Store blob reference both use precisely
	// sha256:<lowercase hex>; the digest IS the canonical blob reference.
	wire, err := h.blobs.GetBlob(digest)
	if err != nil || ctx.Err() != nil {
		return ContributionDeliveryPolicy{}, delivery.ErrDenied
	}
	p, err := decodeContributionDeliveryPolicy(wire, digest)
	if err != nil || ctx.Err() != nil {
		return ContributionDeliveryPolicy{}, delivery.ErrDenied
	}
	return p, nil
}

// Only trusted accepted-candidate callbacks call this unexported writer. Policy
// Binding contains admission metadata, not CredentialBinding or secret values.
// A later denial may leave an orphan immutable blob; this is not cross-store atomicity.
func (s *Service) retainContributionDeliveryPolicy(ctx context.Context, p ContributionDeliveryPolicy) error {
	h, err := NewContributionDeliveryPolicyHistory(s)
	if err != nil || ctx == nil || ctx.Err() != nil {
		return delivery.ErrDenied
	}
	p = p.snapshot()
	wire, err := json.Marshal(p)
	if err != nil {
		return delivery.ErrDenied
	}
	digest := p.Digest()
	if _, err = decodeContributionDeliveryPolicy(wire, digest); err != nil {
		return delivery.ErrDenied
	}
	ref, err := h.blobs.PutBlob(wire)
	if err != nil || ref != digest || ctx.Err() != nil {
		return delivery.ErrDenied
	}
	stored, err := h.ResolveContributionDeliveryPolicyDigest(ctx, digest)
	if err != nil || stored.Digest() != digest || stored.Binding != p.Binding {
		return delivery.ErrDenied
	}
	return nil
}
