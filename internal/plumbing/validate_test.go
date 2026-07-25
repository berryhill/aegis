package plumbing

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func instant(value time.Time) *time.Time { return &value }

func validAggregate() Aggregate {
	base := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	provenance := func(source string, at time.Time) Provenance {
		return Provenance{OwnerID: "owner-1", Producer: "aegis-control-plane", SourceRef: source, RecordedAt: at}
	}
	authority := AuthorityContext{
		ID:               "authority-1",
		MandateID:        "mandate-1",
		DecisionID:       "decision-1",
		ParticipantID:    "participant-1",
		AgentID:          "agent-1",
		StanzaID:         "principal",
		CharterRevision:  3,
		CharterDigest:    digest("charter"),
		Runtime:          "hermes-agent",
		RuntimeVersion:   "0.18.0",
		Capabilities:     []string{"chat", "github/read"},
		Tools:            []string{"aegis"},
		MemoryScopes:     []string{"principal"},
		CredentialScopes: []string{"github/read"},
		IssuedAt:         base.Add(3 * time.Second),
		ExpiresAt:        base.Add(5 * time.Minute),
		Provenance:       provenance("mandate:mandate-1", base.Add(3*time.Second)),
	}
	authority.Digest = AuthorityDigest(authority)

	return Aggregate{
		SchemaVersion: SchemaVersion,
		ID:            "lifecycle-1",
		Revision:      1,
		OwnerID:       "owner-1",
		Participant: Participant{
			ID:          "participant-1",
			Kind:        "human",
			PrincipalID: "principal-1",
			Authentication: Authentication{
				EvidenceID:      "authentication-evidence-1",
				Issuer:          "local-os",
				Method:          "local-os",
				AuthenticatedAt: base,
				ExpiresAt:       base.Add(10 * time.Minute),
				ClaimsDigest:    digest("claims"),
			},
			Provenance: provenance("peercred:1", base),
		},
		Ingress: IngressFact{
			ID:            "ingress-1",
			ParticipantID: "participant-1",
			ContactID:     "contact-1",
			ChannelID:     "channel-1",
			ChannelKind:   "unix-socket",
			EndpointRef:   "listener:control",
			ObservedAt:    base.Add(time.Second),
			Provenance:    provenance("peercred:1", base.Add(time.Second)),
		},
		Decision: StanzaDecision{
			ID:               "decision-1",
			ParticipantID:    "participant-1",
			IngressFactID:    "ingress-1",
			AgentID:          "agent-1",
			CharterRevision:  3,
			CharterDigest:    digest("charter"),
			Allowed:          true,
			MatchingCount:    1,
			SelectedStanzaID: "principal",
			Reason:           "exact_authorized_match",
			DecidedAt:        base.Add(2 * time.Second),
			Provenance:       provenance("selector:decision-1", base.Add(2*time.Second)),
		},
		Authority: &authority,
		Attempts: []Attempt{
			{
				ID:                 "dispatch-1",
				Kind:               AttemptDispatch,
				AuthorityContextID: "authority-1",
				RuntimeAttemptID:   "runtime-dispatch-1",
				State:              AttemptSucceeded,
				RequestedAt:        base.Add(4 * time.Second),
				StartedAt:          instant(base.Add(5 * time.Second)),
				FinishedAt:         instant(base.Add(6 * time.Second)),
				Provenance:         provenance("dispatcher:1", base.Add(6*time.Second)),
			},
			{
				ID:                 "session-1",
				Kind:               AttemptSession,
				ParentAttemptID:    "dispatch-1",
				AuthorityContextID: "authority-1",
				RuntimeAttemptID:   "hermes-session-1",
				State:              AttemptSucceeded,
				RequestedAt:        base.Add(7 * time.Second),
				StartedAt:          instant(base.Add(8 * time.Second)),
				FinishedAt:         instant(base.Add(14 * time.Second)),
				Provenance:         provenance("runtime:1", base.Add(14*time.Second)),
			},
		},
		Operations: []Operation{{
			ID:                 "operation-1",
			AuthorityContextID: "authority-1",
			SessionAttemptID:   "session-1",
			Type:               "github.get_repository",
			SchemaVersion:      "v1",
			ParametersDigest:   digest("owner/repository"),
			ParametersRef:      "parameters:operation-1",
			CreatedAt:          base.Add(9 * time.Second),
			Provenance:         provenance("broker:operation-1", base.Add(9*time.Second)),
		}},
		Requests: []Request{{
			ID:            "request-1",
			OperationID:   "operation-1",
			PayloadDigest: digest("request"),
			PayloadRef:    "payload:request-1",
			Deadline:      base.Add(20 * time.Second),
			CreatedAt:     base.Add(10 * time.Second),
			Provenance:    provenance("broker:request-1", base.Add(10*time.Second)),
		}},
		Artifacts: []Artifact{{
			ID:         "artifact-1",
			RequestID:  "request-1",
			OwnerID:    "participant-1",
			Kind:       "repository-metadata",
			Revision:   1,
			Digest:     digest("artifact"),
			ContentRef: "artifact:artifact-1",
			MediaType:  "application/json",
			CreatedAt:  base.Add(11 * time.Second),
			Provenance: provenance("broker:artifact-1", base.Add(11*time.Second)),
		}},
		Deliveries: []Delivery{{
			ID:                 "delivery-1",
			RequestID:          "request-1",
			ArtifactID:         "artifact-1",
			AuthorityContextID: "authority-1",
			Destination:        "participant:participant-1",
			State:              DeliveryDelivered,
			AttemptedAt:        base.Add(12 * time.Second),
			FinishedAt:         instant(base.Add(13 * time.Second)),
			Provenance:         provenance("delivery:1", base.Add(13*time.Second)),
		}},
		Evidence: []VerificationEvidence{{
			ID:          "evidence-1",
			SubjectKind: "artifact",
			SubjectID:   "artifact-1",
			Verifier:    "aegis-artifact-verifier",
			Outcome:     VerificationPassed,
			Digest:      digest("verified-artifact"),
			EvidenceRef: "verification:evidence-1",
			ObservedAt:  base.Add(14 * time.Second),
			Provenance:  provenance("verifier:1", base.Add(14*time.Second)),
		}},
		Disposition: &TerminalDisposition{
			ID:          "disposition-1",
			State:       DispositionSucceeded,
			Reason:      "verified_delivery_complete",
			EvidenceIDs: []string{"evidence-1"},
			DecidedAt:   base.Add(15 * time.Second),
			Provenance:  provenance("lifecycle:1", base.Add(15*time.Second)),
		},
		CreatedAt: base,
		UpdatedAt: base.Add(15 * time.Second),
	}
}

func TestValidateAcceptsCompleteCausalLifecycle(t *testing.T) {
	if err := Validate(validAggregate()); err != nil {
		t.Fatalf("valid aggregate rejected: %v", err)
	}
}

func TestValidateFailsClosedOnAuthorityAndCausalityBreaks(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Aggregate)
	}{
		{"ambiguous allowed selection", func(a *Aggregate) { a.Decision.MatchingCount = 2 }},
		{"missing authority", func(a *Aggregate) { a.Authority = nil }},
		{"stanza mismatch", func(a *Aggregate) {
			a.Authority.StanzaID = "teamwide"
			a.Authority.Digest = AuthorityDigest(*a.Authority)
		}},
		{"authority tampering", func(a *Aggregate) { a.Authority.Tools = append(a.Authority.Tools, "shell") }},
		{"unsorted authority union", func(a *Aggregate) {
			a.Authority.Tools = []string{"shell", "aegis"}
			a.Authority.Digest = AuthorityDigest(*a.Authority)
		}},
		{"authority outlives authentication", func(a *Aggregate) {
			a.Authority.ExpiresAt = a.Participant.Authentication.ExpiresAt.Add(time.Second)
			a.Authority.Digest = AuthorityDigest(*a.Authority)
		}},
		{"session without successful dispatch", func(a *Aggregate) { a.Attempts[0].State = AttemptFailed }},
		{"session parent appears later", func(a *Aggregate) {
			a.Attempts[0], a.Attempts[1] = a.Attempts[1], a.Attempts[0]
		}},
		{"operation outside session", func(a *Aggregate) { a.Operations[0].SessionAttemptID = "missing" }},
		{"request parent appears later", func(a *Aggregate) {
			child := a.Requests[0]
			child.ID = "request-child"
			child.ParentRequestID = "request-1"
			child.PayloadDigest = digest("child")
			a.Requests = append([]Request{child}, a.Requests...)
		}},
		{"artifact outside request", func(a *Aggregate) { a.Artifacts[0].RequestID = "missing" }},
		{"delivery crosses authority", func(a *Aggregate) { a.Deliveries[0].AuthorityContextID = "other" }},
		{"delivery artifact belongs to another request", func(a *Aggregate) {
			request := a.Requests[0]
			request.ID = "request-2"
			request.PayloadDigest = digest("request-2")
			a.Requests = append(a.Requests, request)
			a.Deliveries[0].RequestID = "request-2"
		}},
		{"evidence lies about subject kind", func(a *Aggregate) { a.Evidence[0].SubjectKind = "request" }},
		{"unknown terminal evidence", func(a *Aggregate) { a.Disposition.EvidenceIDs[0] = "missing" }},
		{"terminal disposition predates evidence", func(a *Aggregate) { a.Disposition.DecidedAt = a.Evidence[0].ObservedAt.Add(-time.Second) }},
		{"terminal disposition predates stanza decision", func(a *Aggregate) {
			a.Decision.Allowed = false
			a.Decision.MatchingCount = 0
			a.Decision.SelectedStanzaID = ""
			a.Authority = nil
			a.Attempts = nil
			a.Operations = nil
			a.Requests = nil
			a.Artifacts = nil
			a.Deliveries = nil
			a.Evidence = nil
			a.Disposition.State = DispositionDenied
			a.Disposition.EvidenceIDs = nil
			a.Disposition.DecidedAt = a.Decision.DecidedAt.Add(-time.Nanosecond)
		}},
		{"successful disposition without passed evidence", func(a *Aggregate) { a.Evidence[0].Outcome = VerificationFailed }},
		{"cross-owner provenance", func(a *Aggregate) { a.Requests[0].Provenance.OwnerID = "other-owner" }},
		{"model cannot author decision provenance", func(a *Aggregate) { a.Decision.Provenance.Producer = ProvenanceProducer("model") }},
		{"runtime narration cannot author evidence provenance", func(a *Aggregate) { a.Evidence[0].Provenance.Producer = ProvenanceProducer("runtime-narration") }},
		{"model cannot act as verifier", func(a *Aggregate) { a.Evidence[0].Verifier = VerifierID("model") }},
		{"operation after authority expiry", func(a *Aggregate) { a.Operations[0].CreatedAt = a.Authority.ExpiresAt }},
		{"decision predates ingress", func(a *Aggregate) { a.Decision.DecidedAt = a.Ingress.ObservedAt.Add(-time.Nanosecond) }},
		{"session predates dispatch completion", func(a *Aggregate) { a.Attempts[1].RequestedAt = a.Attempts[0].FinishedAt.Add(-time.Nanosecond) }},
		{"operation predates session start", func(a *Aggregate) { a.Operations[0].CreatedAt = a.Attempts[1].StartedAt.Add(-time.Nanosecond) }},
		{"request predates operation", func(a *Aggregate) { a.Requests[0].CreatedAt = a.Operations[0].CreatedAt.Add(-time.Nanosecond) }},
		{"child request predates parent", func(a *Aggregate) {
			child := a.Requests[0]
			child.ID = "request-child"
			child.ParentRequestID = a.Requests[0].ID
			child.CreatedAt = a.Requests[0].CreatedAt.Add(-time.Nanosecond)
			child.PayloadDigest = digest("child")
			a.Requests = append(a.Requests, child)
		}},
		{"artifact at authority expiry", func(a *Aggregate) { a.Artifacts[0].CreatedAt = a.Authority.ExpiresAt }},
		{"evidence predates subject", func(a *Aggregate) {
			a.Evidence[0].ObservedAt = a.Artifacts[0].CreatedAt.Add(-time.Nanosecond)
		}},
		{"provenance predates fact", func(a *Aggregate) {
			a.Requests[0].Provenance.RecordedAt = a.Requests[0].CreatedAt.Add(-time.Nanosecond)
		}},
		{"delivery attempted at authority expiry", func(a *Aggregate) { a.Deliveries[0].AttemptedAt = a.Authority.ExpiresAt }},
		{"delivery completes at request deadline", func(a *Aggregate) { a.Deliveries[0].FinishedAt = instant(a.Requests[0].Deadline) }},
		{"attempt starts at authority expiry", func(a *Aggregate) {
			a.Attempts[1].StartedAt = instant(a.Authority.ExpiresAt)
			a.Attempts[1].FinishedAt = instant(a.Authority.ExpiresAt.Add(time.Second))
			a.Attempts[1].Provenance.RecordedAt = a.Authority.ExpiresAt.Add(time.Second)
			a.UpdatedAt = a.Authority.ExpiresAt.Add(time.Second)
		}},
		{"successful attempt finishes at authority expiry", func(a *Aggregate) {
			a.Attempts[1].FinishedAt = instant(a.Authority.ExpiresAt)
			a.UpdatedAt = a.Authority.ExpiresAt
		}},
		{"operation at revocation boundary", func(a *Aggregate) {
			revokedAt := a.Operations[0].CreatedAt
			a.Revocations = []AuthorityRevocation{{
				ID: "revocation-1", AuthorityContextID: a.Authority.ID, Reason: "operator_revoked", RevokedAt: revokedAt,
				Provenance: Provenance{OwnerID: a.OwnerID, Producer: "aegis-control-plane", SourceRef: "revocation:1", RecordedAt: revokedAt},
			}}
		}},
		{"successful delivery finishes at revocation boundary", func(a *Aggregate) {
			revokedAt := *a.Deliveries[0].FinishedAt
			a.Revocations = []AuthorityRevocation{{
				ID: "revocation-1", AuthorityContextID: a.Authority.ID, Reason: "operator_revoked", RevokedAt: revokedAt,
				Provenance: Provenance{OwnerID: a.OwnerID, Producer: "aegis-control-plane", SourceRef: "revocation:1", RecordedAt: revokedAt},
			}}
		}},
		{"revocation at authority expiry", func(a *Aggregate) {
			revokedAt := a.Authority.ExpiresAt
			a.UpdatedAt = revokedAt
			a.Revocations = []AuthorityRevocation{{
				ID: "revocation-1", AuthorityContextID: a.Authority.ID, Reason: "operator_revoked", RevokedAt: revokedAt,
				Provenance: Provenance{OwnerID: a.OwnerID, Producer: "aegis-control-plane", SourceRef: "revocation:1", RecordedAt: revokedAt},
			}}
		}},
		{"successful disposition follows revocation", func(a *Aggregate) {
			revokedAt := a.Evidence[0].ObservedAt.Add(500 * time.Millisecond)
			a.Revocations = []AuthorityRevocation{{
				ID: "revocation-1", AuthorityContextID: a.Authority.ID, Reason: "operator_revoked", RevokedAt: revokedAt,
				Provenance: Provenance{OwnerID: a.OwnerID, Producer: "aegis-control-plane", SourceRef: "revocation:1", RecordedAt: revokedAt},
			}}
		}},
		{"active aggregate outlives authority", func(a *Aggregate) {
			a.Disposition = nil
			a.UpdatedAt = a.Authority.ExpiresAt
		}},
		{"successful disposition with active session", func(a *Aggregate) {
			a.Attempts[1].State = AttemptStarted
			a.Attempts[1].FinishedAt = nil
		}},
		{"successful disposition without delivered artifact", func(a *Aggregate) { a.Deliveries[0].State = DeliveryFailed }},
		{"failed disposition without failure", func(a *Aggregate) { a.Disposition.State = DispositionFailed }},
		{"revocation crosses authority", func(a *Aggregate) {
			a.Revocations = []AuthorityRevocation{{
				ID: "revocation-1", AuthorityContextID: "other", Reason: "operator_revoked",
				RevokedAt:  a.Authority.IssuedAt.Add(time.Second),
				Provenance: Provenance{OwnerID: a.OwnerID, Producer: "aegis-control-plane", SourceRef: "revocation:1", RecordedAt: a.Authority.IssuedAt.Add(time.Second)},
			}}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			aggregate := validAggregate()
			test.edit(&aggregate)
			if err := Validate(aggregate); !errors.Is(err, ErrInvalid) {
				t.Fatalf("unsafe aggregate accepted: %v", err)
			}
		})
	}
}

func TestValidateRejectsAttemptStartingAtAuthorityExpiry(t *testing.T) {
	a := validAggregate()
	a.Attempts[1].StartedAt = instant(a.Authority.ExpiresAt)
	a.Attempts[1].FinishedAt = instant(a.Authority.ExpiresAt.Add(time.Second))
	a.Attempts[1].Provenance.RecordedAt = a.Authority.ExpiresAt.Add(time.Second)
	a.UpdatedAt = a.Authority.ExpiresAt.Add(time.Second)

	err := Validate(a)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "started after authority ceased to be effective") {
		t.Fatalf("attempt start at authority expiry was not rejected at the authority boundary: %v", err)
	}
}

func TestValidateAllowsCleanupAfterAuthorityEnds(t *testing.T) {
	for _, test := range []struct {
		name string
		end  func(*Aggregate, time.Time)
	}{
		{
			name: "revocation",
			end: func(a *Aggregate, endedAt time.Time) {
				a.Revocations = []AuthorityRevocation{{
					ID: "revocation-1", AuthorityContextID: a.Authority.ID, Reason: "operator_revoked", RevokedAt: endedAt,
					Provenance: Provenance{OwnerID: a.OwnerID, Producer: "aegis-control-plane", SourceRef: "revocation:1", RecordedAt: endedAt},
				}}
			},
		},
		{
			name: "expiry",
			end: func(a *Aggregate, endedAt time.Time) {
				a.Authority.ExpiresAt = endedAt
				a.Authority.Digest = AuthorityDigest(*a.Authority)
				a.Requests[0].Deadline = endedAt
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := validAggregate()
			endedAt := a.Deliveries[0].AttemptedAt.Add(500 * time.Millisecond)
			test.end(&a, endedAt)

			failedAt := endedAt.Add(500 * time.Millisecond)
			a.Attempts[1].State = AttemptFailed
			a.Attempts[1].FinishedAt = instant(failedAt)
			a.Attempts[1].Provenance.RecordedAt = failedAt
			a.Deliveries[0].State = DeliveryFailed
			a.Deliveries[0].FinishedAt = instant(failedAt)
			a.Deliveries[0].Provenance.RecordedAt = failedAt
			a.Evidence[0].Outcome = VerificationFailed
			a.Disposition.State = DispositionFailed
			a.Disposition.Reason = "cleanup_after_authority_end"

			if err := Validate(a); err != nil {
				t.Fatalf("post-%s cleanup rejected: %v", test.name, err)
			}
		})
	}
}

func TestValidateAllowsFailedDeliveryRetryBeforeSuccess(t *testing.T) {
	a := validAggregate()
	attemptedAt := a.Artifacts[0].CreatedAt.Add(100 * time.Millisecond)
	finishedAt := attemptedAt.Add(100 * time.Millisecond)
	failed := a.Deliveries[0]
	failed.ID = "delivery-failed-1"
	failed.State = DeliveryFailed
	failed.AttemptedAt = attemptedAt
	failed.FinishedAt = instant(finishedAt)
	failed.Reason = "transient_transport_failure"
	failed.Provenance.SourceRef = "delivery:failed-1"
	failed.Provenance.RecordedAt = finishedAt
	a.Deliveries = append([]Delivery{failed}, a.Deliveries...)

	if err := Validate(a); err != nil {
		t.Fatalf("successful retry lifecycle rejected: %v", err)
	}
}

func TestDeniedSelectionCannotCreateAuthorityOrWork(t *testing.T) {
	for _, test := range []struct {
		name    string
		matches uint32
		reason  string
	}{
		{name: "zero matches", matches: 0, reason: "no_authorized_match"},
		{name: "ambiguous matches", matches: 2, reason: "multiple_authorized_matches"},
	} {
		t.Run(test.name, func(t *testing.T) {
			aggregate := validAggregate()
			aggregate.Decision.Allowed = false
			aggregate.Decision.MatchingCount = test.matches
			aggregate.Decision.SelectedStanzaID = ""
			aggregate.Decision.Reason = test.reason
			aggregate.Authority = nil
			aggregate.Attempts = nil
			aggregate.Operations = nil
			aggregate.Requests = nil
			aggregate.Artifacts = nil
			aggregate.Deliveries = nil
			aggregate.Evidence = nil
			aggregate.Disposition.State = DispositionDenied
			aggregate.Disposition.Reason = test.reason
			aggregate.Disposition.EvidenceIDs = nil
			if err := Validate(aggregate); err != nil {
				t.Fatalf("complete denial rejected: %v", err)
			}

			aggregate.Authority = validAggregate().Authority
			if err := Validate(aggregate); !errors.Is(err, ErrInvalid) {
				t.Fatalf("authority issued after denial: %v", err)
			}
		})
	}
}

func TestAuthorityDigestBindsImmutableContext(t *testing.T) {
	authority := *validAggregate().Authority
	original := authority.Digest

	authority.Digest = digest("untrusted-digest-field")
	if got := AuthorityDigest(authority); got != original {
		t.Fatalf("digest field must not recursively affect authority digest: got %s want %s", got, original)
	}

	authority.StanzaID = "teamwide"
	if got := AuthorityDigest(authority); got == original {
		t.Fatal("stanza mutation did not change authority digest")
	}
}

func TestLifecycleTransitionsAreMonotonicAndTerminal(t *testing.T) {
	for _, transition := range [][2]AttemptState{
		{AttemptRequested, AttemptStarted},
		{AttemptRequested, AttemptDenied},
		{AttemptStarted, AttemptSucceeded},
		{AttemptStarted, AttemptFailed},
	} {
		if !CanTransitionAttempt(transition[0], transition[1]) {
			t.Fatalf("expected attempt transition %s -> %s", transition[0], transition[1])
		}
	}
	for _, transition := range [][2]AttemptState{
		{AttemptRequested, AttemptSucceeded},
		{AttemptSucceeded, AttemptStarted},
		{AttemptDenied, AttemptRequested},
		{AttemptFailed, AttemptSucceeded},
	} {
		if CanTransitionAttempt(transition[0], transition[1]) {
			t.Fatalf("unsafe attempt transition allowed %s -> %s", transition[0], transition[1])
		}
	}
	if !CanTransitionDelivery(DeliveryPending, DeliveryDelivered) || CanTransitionDelivery(DeliveryDelivered, DeliveryPending) || CanTransitionDelivery(DeliveryFailed, DeliveryDelivered) {
		t.Fatal("delivery lifecycle is not monotonic and terminal")
	}
}
