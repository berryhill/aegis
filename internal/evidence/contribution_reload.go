package evidence

import (
	"context"
)

// ReloadContributionManifest is an independent blob read with exact expected
// bindings. It is deliberately not a completion authorization: producer-owned
// native, authority and delivery records still have to be reloaded and verified.
func (v *BlobVerifier) ReloadContributionManifest(ctx context.Context, ref string, expected ContributionBinding, target string) (ContributionManifest, error) {
	if v == nil || v.store == nil || ctx.Err() != nil || expected.Validate() != nil {
		return ContributionManifest{}, ErrContributionEvidence
	}
	wire, err := v.store.GetBlob(ref)
	if err != nil {
		return ContributionManifest{}, ErrContributionEvidence
	}
	m, err := DecodeContributionManifest(ref, wire)
	if err != nil || m.Binding != expected || m.Target != target {
		return ContributionManifest{}, ErrContributionEvidence
	}
	return m, nil
}

// ReloadContributionReceipt rejects cross-run/candidate/role-policy substitution
// and exact-set mismatches, even when the substituted blob itself hashes and
// decodes correctly. This read verifies retained report integrity, not the
// provenance that originally authorized the report or any new terminal effect.
func (v *BlobVerifier) ReloadContributionReceipt(ctx context.Context, ref string, expected ContributionBinding, manifest string, required []string) (ContributionReceipt, error) {
	if v == nil || v.store == nil || ctx.Err() != nil || expected.Validate() != nil || !validDigest(manifest) || required == nil {
		return ContributionReceipt{}, ErrContributionEvidence
	}
	wire, err := v.store.GetBlob(ref)
	if err != nil {
		return ContributionReceipt{}, ErrContributionEvidence
	}
	r, err := DecodeContributionReceipt(ref, wire)
	if err != nil || r.Binding != expected || r.Manifest != manifest || len(r.Evidence) != len(required) {
		return ContributionReceipt{}, ErrContributionEvidence
	}
	// Caller-selected evidence cannot widen the immutable target or omit its proof.
	m, err := v.ReloadContributionManifest(ctx, r.Manifest, expected, r.Boundary)
	if err != nil {
		return ContributionReceipt{}, ErrContributionEvidence
	}
	seen := map[string]bool{}
	for _, x := range required {
		if !validDigest(x) || seen[x] {
			return ContributionReceipt{}, ErrContributionEvidence
		}
		seen[x] = true
	}
	obligations := append([]string{m.CandidateEvidence, m.AuthorityEvidence}, m.DeliveryEvidence...)
	if m.IssueEvidence != "" {
		obligations = append(obligations, m.IssueEvidence)
	}
	if len(required) != len(obligations) {
		return ContributionReceipt{}, ErrContributionEvidence
	}
	for _, ref := range obligations {
		if !seen[ref] {
			return ContributionReceipt{}, ErrContributionEvidence
		}
	}
	for _, x := range r.Evidence {
		if !seen[x] {
			return ContributionReceipt{}, ErrContributionEvidence
		}
		delete(seen, x)
	}
	if len(seen) != 0 || ctx.Err() != nil {
		return ContributionReceipt{}, ErrContributionEvidence
	}
	return r, nil
}
