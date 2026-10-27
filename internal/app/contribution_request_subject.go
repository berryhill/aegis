package app

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/delivery"
)

// contributionNativeRequest is minted only at the existing kernel-peer
// authentication boundary. It is not serializable or constructible from a
// supplied Subject, a manager token, password login, or startup configuration.
type contributionNativeRequestKey struct{}
type contributionNativeRequest struct {
	service *Service
	subject core.Subject
}

// AuthenticateContributionUnixPeerRequest preserves the provenance of the
// existing AuthenticateUnixPeer transport. As with that API, uid MUST come from
// SO_PEERCRED after protected transport admission, never request data. The
// returned context belongs to this request; it does not renew a manager entry,
// role, mandate, credential, or historical permit.
func (s *Service) AuthenticateContributionUnixPeerRequest(ctx context.Context, uid uint32) (context.Context, core.Subject, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, core.Subject{}, delivery.ErrDenied
	}
	sub, err := s.AuthenticateUnixPeer(ctx, uid)
	if err != nil {
		return nil, core.Subject{}, err
	}
	wire, err := json.Marshal(sub)
	if err != nil {
		return nil, core.Subject{}, err
	}
	var snapshot core.Subject
	if err = json.Unmarshal(wire, &snapshot); err != nil {
		return nil, core.Subject{}, err
	}
	return context.WithValue(ctx, contributionNativeRequestKey{}, contributionNativeRequest{s, snapshot}), sub, nil
}

type contributionRequestSubjectKey struct{}
type contributionRequestSubject struct {
	service *Service
	subject core.Subject
}

func (s *Service) validateContributionRequestSubject(ctx context.Context, sub core.Subject) error {
	if s == nil || ctx == nil || ctx.Err() != nil || s.requirePrincipal(sub) != nil {
		return delivery.ErrDenied
	}
	// Password authority belongs to the live owning Console registry, not a
	// timestamp or a manager session opened from a caller-constructed Subject.
	if sub.Method == "password" {
		if s.DashboardActionAuthentication == nil || s.DashboardActionAuthentication(sub) != nil {
			return delivery.ErrDenied
		}
		return nil
	}
	proof, ok := ctx.Value(contributionNativeRequestKey{}).(contributionNativeRequest)
	if !ok || proof.service != s || s.requirePrincipal(proof.subject) != nil ||
		sub.Method != "local-os" || sub.Issuer != "linux-so-peercred" || sub.Kind != "human" {
		return delivery.ErrDenied
	}
	// Each HTTP request is authenticated again. Compare immutable identity and
	// claims, NOT timestamps: the manager retains its original hard expiry and
	// may never inherit this request's later authentication window.
	a, b := sub, proof.subject
	a.AuthenticatedAt, a.ExpiresAt = b.AuthenticatedAt, b.ExpiresAt
	aw, _ := json.Marshal(a)
	bw, _ := json.Marshal(b)
	if string(aw) != string(bw) || sub.AuthenticatedAt.IsZero() || sub.AuthenticatedAt.After(proof.subject.AuthenticatedAt) ||
		sub.ExpiresAt.After(proof.subject.ExpiresAt) || sub.ExpiresAt.After(sub.AuthenticatedAt.Add(s.Config.Principal.AuthTTL)) {
		return delivery.ErrDenied
	}
	return nil
}

// Only admitted owning-service entry points can bind this private request port.
// No public helper can turn a Subject value into an authenticated context.
func (s *Service) bindContributionRequestSubject(ctx context.Context, sub core.Subject) (context.Context, error) {
	if err := s.validateContributionRequestSubject(ctx, sub); err != nil {
		return nil, err
	}
	if old, ok := ctx.Value(contributionRequestSubjectKey{}).(contributionRequestSubject); ok {
		a, _ := json.Marshal(old.subject)
		b, _ := json.Marshal(sub)
		if old.service != s || string(a) != string(b) {
			return nil, delivery.ErrDenied
		}
	}
	// Clone claims: the caller cannot mutate bound authority after admission.
	wire, _ := json.Marshal(sub)
	var snapshot core.Subject
	if json.Unmarshal(wire, &snapshot) != nil {
		return nil, delivery.ErrDenied
	}
	return context.WithValue(ctx, contributionRequestSubjectKey{}, contributionRequestSubject{s, snapshot}), nil
}

// CurrentContributionDeliverySubject extracts and revalidates the current
// owning request. It never authenticates, renews or caches startup authority.
func (s *Service) CurrentContributionDeliverySubject(ctx context.Context) (core.Subject, error) {
	if ctx == nil {
		return core.Subject{}, delivery.ErrDenied
	}
	bound, ok := ctx.Value(contributionRequestSubjectKey{}).(contributionRequestSubject)
	if !ok || bound.service != s || s.validateContributionRequestSubject(ctx, bound.subject) != nil {
		return core.Subject{}, delivery.ErrDenied
	}
	wire, _ := json.Marshal(bound.subject)
	var sub core.Subject
	if json.Unmarshal(wire, &sub) != nil {
		return core.Subject{}, delivery.ErrDenied
	}
	return sub, nil
}
