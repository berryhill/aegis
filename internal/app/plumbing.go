package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/plumbing"
	"github.com/berryhill/aegis/internal/poc"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
)

// PlumbingPOCInput is an explicitly non-production, non-restrictive proof
// request. Acknowledge does not authenticate the caller or select authority.
type PlumbingPOCInput struct {
	Prompt      string
	Expected    string
	Provider    string
	Model       string
	Acknowledge bool
}

func (s *Service) RunPlumbingPOC(ctx context.Context, subject core.Subject, input PlumbingPOCInput) (plumbing.Aggregate, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return plumbing.Aggregate{}, err
	}
	if !input.Acknowledge {
		return plumbing.Aggregate{}, errors.New("explicit acknowledgement of the non-production unrestricted plumbing proof is required")
	}
	if strings.TrimSpace(input.Prompt) == "" || strings.TrimSpace(input.Expected) == "" || strings.TrimSpace(input.Provider) == "" || strings.TrimSpace(input.Model) == "" {
		return plumbing.Aggregate{}, errors.New("prompt, expected output, provider, and model are required")
	}
	descriptor, err := s.Hermes.Discover(ctx)
	if err != nil {
		return plumbing.Aggregate{}, err
	}
	credentials, err := s.resolveProviderCredential(input.Provider, []string{"provider:" + input.Provider})
	if err != nil {
		return plumbing.Aggregate{}, err
	}
	now := s.Now().UTC()
	if !now.Before(subject.ExpiresAt) {
		return plumbing.Aggregate{}, ErrExpired
	}
	expiresAt := now.Add(2 * time.Minute)
	if subject.ExpiresAt.Before(expiresAt) {
		expiresAt = subject.ExpiresAt
	}
	owner := s.Config.Principal.ID
	createdAt := subject.AuthenticatedAt
	ingressAt := createdAt.Add(time.Nanosecond)
	decisionAt := createdAt.Add(2 * time.Nanosecond)
	issuedAt := createdAt.Add(3 * time.Nanosecond)
	dispatchRequestedAt := createdAt.Add(4 * time.Nanosecond)
	dispatchStartedAt := createdAt.Add(5 * time.Nanosecond)
	dispatchFinishedAt := createdAt.Add(6 * time.Nanosecond)
	participantID := store.ID("participant")
	ingressID := store.ID("ingress")
	decisionID := store.ID("decision")
	authorityID := store.ID("authority")
	dispatchID := store.ID("attempt")
	charterDigest := strings.TrimPrefix(core.Digest(map[string]any{"agent": "plumbing-poc", "revision": 1}), "sha256:")
	control := func(source string, at time.Time) plumbing.Provenance {
		return plumbing.Provenance{OwnerID: owner, Producer: plumbing.ProducerControlPlane, SourceRef: source, RecordedAt: at}
	}
	authority := plumbing.AuthorityContext{
		ID: authorityID, MandateID: store.ID("mandate"), DecisionID: decisionID,
		ParticipantID: participantID, AgentID: "plumbing-poc", StanzaID: "explicit-unrestricted-poc",
		CharterRevision: 1, CharterDigest: charterDigest, Runtime: descriptor.Runtime, RuntimeVersion: descriptor.Version,
		Capabilities: []string{"chat"}, Tools: []string{}, MemoryScopes: []string{}, CredentialScopes: []string{"provider:" + input.Provider},
		IssuedAt: issuedAt, ExpiresAt: expiresAt, Provenance: control("plumbing-poc-mandate", issuedAt),
	}
	authority.Digest = plumbing.AuthorityDigest(authority)
	aggregate := plumbing.Aggregate{
		SchemaVersion: plumbing.SchemaVersion, ID: store.ID("graph-run"), Revision: 1, OwnerID: owner,
		Participant: plumbing.Participant{ID: participantID, Kind: subject.Kind, PrincipalID: subject.PrincipalID,
			Authentication: plumbing.Authentication{EvidenceID: store.ID("auth-evidence"), Issuer: subject.Issuer, Method: subject.Method, AuthenticatedAt: subject.AuthenticatedAt, ExpiresAt: subject.ExpiresAt, ClaimsDigest: strings.TrimPrefix(core.Digest(subject.Claims), "sha256:")},
			Provenance:     control("authenticated-subject:"+subject.ID, createdAt)},
		Ingress:   plumbing.IngressFact{ID: ingressID, ParticipantID: participantID, ContactID: subject.ID, ChannelID: "aegis-cli-api", ChannelKind: "authenticated-local", EndpointRef: "aegis-control-plane", ObservedAt: ingressAt, Provenance: control("authenticated-ingress", ingressAt)},
		Decision:  plumbing.StanzaDecision{ID: decisionID, ParticipantID: participantID, IngressFactID: ingressID, AgentID: "plumbing-poc", CharterRevision: 1, CharterDigest: charterDigest, Allowed: true, MatchingCount: 1, SelectedStanzaID: authority.StanzaID, Reason: "explicit_nonproduction_poc", DecidedAt: decisionAt, Provenance: control("plumbing-poc-selector", decisionAt)},
		Authority: &authority,
		Attempts:  []plumbing.Attempt{{ID: dispatchID, Kind: plumbing.AttemptDispatch, AuthorityContextID: authorityID, RuntimeAttemptID: "aegis-local-dispatch", State: plumbing.AttemptSucceeded, RequestedAt: dispatchRequestedAt, StartedAt: &dispatchStartedAt, FinishedAt: &dispatchFinishedAt, Provenance: control("plumbing-poc-dispatch", dispatchFinishedAt)}},
		CreatedAt: createdAt, UpdatedAt: dispatchFinishedAt,
	}
	verifier, err := poc.NewBlobArtifactVerifier(s.Store)
	if err != nil {
		return plumbing.Aggregate{}, err
	}
	service, err := poc.New(s.Hermes, verifier, s.Store)
	if err != nil {
		return plumbing.Aggregate{}, err
	}
	return service.Run(ctx, poc.RunInput{
		Aggregate: aggregate, ParentAttemptID: dispatchID, StateRoot: s.Config.StateDir,
		Prompt: input.Prompt, Expected: input.Expected, OperationType: "plumbing.poc.turn", OperationSchema: "aegis.dev/plumbing-poc/v1alpha1",
		Destination: "participant:" + participantID, Model: input.Model, Provider: input.Provider, Credentials: credentials,
		Bounds: hermes.AttemptBounds{InputBytes: hermes.MaxAttemptInputBytes, OutputBytes: hermes.MaxAttemptOutputBytes, Duration: 2 * time.Minute},
	})
}

func (s *Service) ReadGraphRun(ctx context.Context, subject core.Subject, id string) (plumbing.Aggregate, error) {
	if err := s.requirePrincipal(subject); err != nil {
		return plumbing.Aggregate{}, err
	}
	verifier, err := poc.NewBlobArtifactVerifier(s.Store)
	if err != nil {
		return plumbing.Aggregate{}, err
	}
	service, err := poc.New(s.Hermes, verifier, s.Store)
	if err != nil {
		return plumbing.Aggregate{}, err
	}
	return service.Read(ctx, id)
}
