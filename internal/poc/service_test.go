package poc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/plumbing"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
)

type recordingRunner struct {
	calls    int
	requests []hermes.AttemptTurnRequest
	result   hermes.AttemptTurnResult
	err      error
}

func (r *recordingRunner) AttemptTurn(_ context.Context, request hermes.AttemptTurnRequest) (hermes.AttemptTurnResult, error) {
	r.calls++
	r.requests = append(r.requests, request)
	result := r.result
	if result.Attempt.ID == "use-request-id" {
		result.Attempt.ID = request.AttemptID
	}
	return result, r.err
}

type recordingVerifier struct {
	calls     int
	artifacts []plumbing.Artifact
	result    plumbing.VerificationEvidence
	err       error
}

func (v *recordingVerifier) Verify(_ context.Context, artifact plumbing.Artifact) (plumbing.VerificationEvidence, error) {
	v.calls++
	v.artifacts = append(v.artifacts, artifact)
	result := v.result
	if result.SubjectID == "use-artifact-id" {
		result.SubjectID = artifact.ID
	}
	if result.Provenance.OwnerID == "use-artifact-owner" {
		result.Provenance.OwnerID = artifact.OwnerID
	}
	return result, v.err
}

func TestServiceRunPersistsOneCausallyBoundVerifiedLifecycle(t *testing.T) {
	now := time.Now().UTC()
	aggregate := activeAggregate(now)
	finishedAt := now.Add(time.Second)
	runner := &recordingRunner{result: hermes.AttemptTurnResult{
		Attempt: successfulSessionAttempt("use-request-id", aggregate, finishedAt),
		Output:  "persist this exact output",
	}}
	verifier := &recordingVerifier{result: validEvidence(aggregate.OwnerID, finishedAt.Add(time.Millisecond), plumbing.VerificationPassed)}
	records := openTestStore(t)
	service, err := New(runner, verifier, records)
	if err != nil {
		t.Fatal(err)
	}

	got, err := service.Run(context.Background(), validRunInput(aggregate, records.Root()))
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || len(runner.requests) != 1 {
		t.Fatalf("Hermes calls = %d, want exactly one", runner.calls)
	}
	if verifier.calls != 1 || len(verifier.artifacts) != 1 {
		t.Fatalf("verifier calls = %d, want exactly one", verifier.calls)
	}
	request := runner.requests[0]
	if request.Aggregate.Authority.ID != aggregate.Authority.ID || request.ParentAttemptID != "dispatch-1" || request.Input != "produce proof" {
		t.Fatalf("runtime request lost authority, parent, or input binding: %#v", request)
	}
	if got.Revision != aggregate.Revision+1 || got.Disposition == nil || got.Disposition.State != plumbing.DispositionSucceeded {
		t.Fatalf("terminal aggregate = %#v", got.Disposition)
	}
	if len(got.Attempts) != 2 || len(got.Operations) != 1 || len(got.Requests) != 1 || len(got.Artifacts) != 1 || len(got.Deliveries) != 1 || len(got.Evidence) != 1 {
		t.Fatalf("unexpected lifecycle cardinality: attempts=%d operations=%d requests=%d artifacts=%d deliveries=%d evidence=%d", len(got.Attempts), len(got.Operations), len(got.Requests), len(got.Artifacts), len(got.Deliveries), len(got.Evidence))
	}
	operation, outbound, artifact, delivery, evidence := got.Operations[0], got.Requests[0], got.Artifacts[0], got.Deliveries[0], got.Evidence[0]
	if operation.AuthorityContextID != aggregate.Authority.ID || operation.SessionAttemptID != got.Attempts[1].ID || outbound.OperationID != operation.ID || artifact.RequestID != outbound.ID || delivery.RequestID != outbound.ID || delivery.ArtifactID != artifact.ID || delivery.AuthorityContextID != aggregate.Authority.ID || evidence.SubjectID != artifact.ID {
		t.Fatalf("causal chain was not preserved: operation=%#v request=%#v artifact=%#v delivery=%#v evidence=%#v", operation, outbound, artifact, delivery, evidence)
	}
	if len(got.Disposition.EvidenceIDs) != 1 || got.Disposition.EvidenceIDs[0] != evidence.ID {
		t.Fatalf("disposition does not cite verifier evidence: %#v", got.Disposition)
	}
	if err = plumbing.Validate(got); err != nil {
		t.Fatalf("persisted lifecycle is invalid: %v", err)
	}
	content, err := records.GetBlob(artifact.ContentRef)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "persist this exact output" {
		t.Fatalf("persisted output = %q", content)
	}
	var storedArtifact plumbing.Artifact
	if err = records.Load(artifactRecordKind, artifact.ID, &storedArtifact); err != nil || storedArtifact.Digest != artifact.Digest {
		t.Fatalf("artifact record readback failed: artifact=%#v err=%v", storedArtifact, err)
	}
	var storedEvidence plumbing.VerificationEvidence
	if err = records.Load(evidenceRecordKind, evidence.ID, &storedEvidence); err != nil || storedEvidence.Provenance.Producer != plumbing.ProducerVerifier {
		t.Fatalf("evidence record readback failed: evidence=%#v err=%v", storedEvidence, err)
	}
	var storedAggregate plumbing.Aggregate
	if err = records.Load(aggregateRecordKind, aggregate.ID+"-r2", &storedAggregate); err != nil || storedAggregate.Disposition == nil || storedAggregate.Disposition.ID != got.Disposition.ID {
		t.Fatalf("aggregate record readback failed: disposition=%#v err=%v", storedAggregate.Disposition, err)
	}
	events, err := records.AuditEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "poc.lifecycle.terminal" || events[0].Outcome != string(plumbing.DispositionSucceeded) || events[0].Metadata["aggregate_id"] != aggregate.ID {
		t.Fatalf("authoritative audit event = %#v", events)
	}
	if err = records.VerifyAudit(); err != nil {
		t.Fatalf("audit chain verification failed: %v", err)
	}
}

func TestServiceRunFailsClosedBeforeHermesForInvalidAuthority(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name string
		edit func(*plumbing.Aggregate)
	}{
		{"missing authority", func(a *plumbing.Aggregate) { a.Authority = nil }},
		{"ambiguous stanza", func(a *plumbing.Aggregate) { a.Decision.MatchingCount = 2 }},
		{"mismatched stanza", func(a *plumbing.Aggregate) { a.Decision.SelectedStanzaID = "teamwide" }},
		{"tampered authority", func(a *plumbing.Aggregate) { a.Authority.Tools = append(a.Authority.Tools, "shell") }},
		{"wrong runtime", func(a *plumbing.Aggregate) {
			a.Authority.Runtime = "other-runtime"
			a.Authority.Digest = plumbing.AuthorityDigest(*a.Authority)
		}},
		{"expired authority", func(a *plumbing.Aggregate) {
			a.Authority.ExpiresAt = now.Add(-time.Second)
			a.Authority.Digest = plumbing.AuthorityDigest(*a.Authority)
		}},
		{"revoked authority", func(a *plumbing.Aggregate) {
			revokedAt := now.Add(-time.Second)
			a.Revocations = []plumbing.AuthorityRevocation{{ID: "revocation-1", AuthorityContextID: a.Authority.ID, Reason: "operator_revoked", RevokedAt: revokedAt, Provenance: pocProvenance(a.OwnerID, "revocation:1", revokedAt)}}
		}},
		{"missing parent dispatch", func(a *plumbing.Aggregate) { a.Attempts = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			aggregate := activeAggregate(now)
			test.edit(&aggregate)
			runner := &recordingRunner{}
			verifier := &recordingVerifier{}
			records := openTestStore(t)
			service, err := New(runner, verifier, records)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Run(context.Background(), validRunInput(aggregate, records.Root()))
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("error = %v, want ErrDenied", err)
			}
			if runner.calls != 0 || verifier.calls != 0 {
				t.Fatalf("denied input reached execution: runner=%d verifier=%d", runner.calls, verifier.calls)
			}
			if _, statErr := os.Stat(filepath.Join(records.Root(), aggregateRecordKind)); !os.IsNotExist(statErr) {
				t.Fatalf("denied input produced lifecycle storage: %v", statErr)
			}
		})
	}
}

func TestServiceRunPersistsTerminalHermesFailureWithoutVerification(t *testing.T) {
	now := time.Now().UTC()
	aggregate := activeAggregate(now)
	finishedAt := now.Add(time.Second)
	attempt := successfulSessionAttempt("use-request-id", aggregate, finishedAt)
	attempt.State = plumbing.AttemptFailed
	attempt.Reason = "gateway_failed"
	runtimeErr := errors.New("gateway failed")
	runner := &recordingRunner{result: hermes.AttemptTurnResult{Attempt: attempt}, err: runtimeErr}
	verifier := &recordingVerifier{}
	records := openTestStore(t)
	service, _ := New(runner, verifier, records)

	got, err := service.Run(context.Background(), validRunInput(aggregate, records.Root()))
	if !errors.Is(err, runtimeErr) {
		t.Fatalf("error = %v, want runtime failure", err)
	}
	if runner.calls != 1 || verifier.calls != 0 {
		t.Fatalf("calls: runner=%d verifier=%d", runner.calls, verifier.calls)
	}
	if got.Disposition == nil || got.Disposition.State != plumbing.DispositionFailed || got.Disposition.Reason != "hermes_attempt_failed" {
		t.Fatalf("failure disposition = %#v", got.Disposition)
	}
	if len(got.Operations)+len(got.Requests)+len(got.Artifacts)+len(got.Deliveries)+len(got.Evidence) != 0 {
		t.Fatalf("runtime failure created consequential records: %#v", got)
	}
	if err = plumbing.Validate(got); err != nil {
		t.Fatalf("terminal failure aggregate is invalid: %v", err)
	}
	var stored plumbing.Aggregate
	if err = records.Load(aggregateRecordKind, aggregate.ID+"-r2", &stored); err != nil || stored.Disposition == nil || stored.Disposition.State != plumbing.DispositionFailed {
		t.Fatalf("failure readback failed: disposition=%#v err=%v", stored.Disposition, err)
	}
}

func TestServiceRunPersistsDistinctVerificationFailure(t *testing.T) {
	now := time.Now().UTC()
	aggregate := activeAggregate(now)
	finishedAt := now.Add(time.Second)
	runner := &recordingRunner{result: hermes.AttemptTurnResult{Attempt: successfulSessionAttempt("use-request-id", aggregate, finishedAt), Output: "untrusted output"}}
	verifier := &recordingVerifier{result: validEvidence(aggregate.OwnerID, finishedAt.Add(time.Millisecond), plumbing.VerificationFailed)}
	records := openTestStore(t)
	service, _ := New(runner, verifier, records)

	got, err := service.Run(context.Background(), validRunInput(aggregate, records.Root()))
	if err == nil || err.Error() != "artifact verification failed" {
		t.Fatalf("error = %v, want artifact verification failure", err)
	}
	if got.Disposition == nil || got.Disposition.State != plumbing.DispositionFailed || got.Evidence[0].Outcome != plumbing.VerificationFailed {
		t.Fatalf("verification failure was not terminalized: disposition=%#v evidence=%#v", got.Disposition, got.Evidence)
	}
	if got.Deliveries[0].State != plumbing.DeliveryDelivered {
		t.Fatalf("artifact persistence delivery was rewritten by verification result: %#v", got.Deliveries[0])
	}
	if err = plumbing.Validate(got); err != nil {
		t.Fatalf("verification-failed aggregate is invalid: %v", err)
	}
}

func activeAggregate(now time.Time) plumbing.Aggregate {
	createdAt := now.Add(-2 * time.Minute)
	ingressAt := createdAt.Add(time.Second)
	decisionAt := createdAt.Add(2 * time.Second)
	issuedAt := createdAt.Add(3 * time.Second)
	dispatchRequestedAt := createdAt.Add(4 * time.Second)
	dispatchStartedAt := createdAt.Add(5 * time.Second)
	dispatchFinishedAt := createdAt.Add(6 * time.Second)
	owner := "owner-1"
	charterDigest := digest([]byte("charter"))
	authority := plumbing.AuthorityContext{
		ID: "authority-1", MandateID: "mandate-1", DecisionID: "decision-1", ParticipantID: "participant-1", AgentID: "agent-1", StanzaID: "principal",
		CharterRevision: 1, CharterDigest: charterDigest, Runtime: "hermes-agent", RuntimeVersion: "0.18.2", Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}, MemoryScopes: []string{}, CredentialScopes: []string{},
		IssuedAt: issuedAt, ExpiresAt: now.Add(5 * time.Minute), Provenance: pocProvenance(owner, "mandate:mandate-1", issuedAt),
	}
	authority.Digest = plumbing.AuthorityDigest(authority)
	return plumbing.Aggregate{
		SchemaVersion: plumbing.SchemaVersion, ID: "lifecycle-1", Revision: 1, OwnerID: owner,
		Participant: plumbing.Participant{ID: "participant-1", Kind: "human", PrincipalID: "principal-1", Authentication: plumbing.Authentication{EvidenceID: "auth-evidence-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: createdAt, ExpiresAt: now.Add(10 * time.Minute), ClaimsDigest: digest([]byte("claims"))}, Provenance: pocProvenance(owner, "peercred:1", createdAt)},
		Ingress:     plumbing.IngressFact{ID: "ingress-1", ParticipantID: "participant-1", ContactID: "contact-1", ChannelID: "channel-1", ChannelKind: "unix-socket", EndpointRef: "listener:control", ObservedAt: ingressAt, Provenance: pocProvenance(owner, "peercred:1", ingressAt)},
		Decision:    plumbing.StanzaDecision{ID: "decision-1", ParticipantID: "participant-1", IngressFactID: "ingress-1", AgentID: "agent-1", CharterRevision: 1, CharterDigest: charterDigest, Allowed: true, MatchingCount: 1, SelectedStanzaID: "principal", Reason: "exact_authorized_match", DecidedAt: decisionAt, Provenance: pocProvenance(owner, "selector:decision-1", decisionAt)},
		Authority:   &authority,
		Attempts:    []plumbing.Attempt{{ID: "dispatch-1", Kind: plumbing.AttemptDispatch, AuthorityContextID: authority.ID, RuntimeAttemptID: "runtime-dispatch-1", State: plumbing.AttemptSucceeded, RequestedAt: dispatchRequestedAt, StartedAt: &dispatchStartedAt, FinishedAt: &dispatchFinishedAt, Provenance: pocProvenance(owner, "dispatcher:1", dispatchFinishedAt)}},
		CreatedAt:   createdAt, UpdatedAt: dispatchFinishedAt,
	}
}

func successfulSessionAttempt(id string, aggregate plumbing.Aggregate, finishedAt time.Time) plumbing.Attempt {
	startedAt := finishedAt.Add(-500 * time.Millisecond)
	return plumbing.Attempt{ID: id, Kind: plumbing.AttemptSession, ParentAttemptID: "dispatch-1", AuthorityContextID: aggregate.Authority.ID, RuntimeAttemptID: "hermes-session-1", State: plumbing.AttemptSucceeded, RequestedAt: startedAt, StartedAt: &startedAt, FinishedAt: &finishedAt, Provenance: plumbing.Provenance{OwnerID: aggregate.OwnerID, Producer: plumbing.ProducerRuntimeAdapter, SourceRef: "hermes-session-1", RecordedAt: finishedAt}}
}

func validEvidence(owner string, observedAt time.Time, outcome plumbing.VerificationOutcome) plumbing.VerificationEvidence {
	return plumbing.VerificationEvidence{ID: "evidence-1", SubjectKind: "artifact", SubjectID: "use-artifact-id", Verifier: plumbing.VerifierArtifact, Outcome: outcome, Digest: digest([]byte("receipt")), EvidenceRef: "sha256:" + digest([]byte("receipt")), ObservedAt: observedAt, Provenance: plumbing.Provenance{OwnerID: "use-artifact-owner", Producer: plumbing.ProducerVerifier, SourceRef: "verifier:1", RecordedAt: observedAt}}
}

func validRunInput(aggregate plumbing.Aggregate, root string) RunInput {
	return RunInput{Aggregate: aggregate, ParentAttemptID: "dispatch-1", StateRoot: filepath.Join(root, "runtime-state"), Prompt: "produce proof", OperationType: "poc.produce", OperationSchema: "v1", Destination: "participant:participant-1", Bounds: hermes.AttemptBounds{InputBytes: 1024, OutputBytes: 1024, Duration: time.Second}}
}

func pocProvenance(owner, source string, at time.Time) plumbing.Provenance {
	return plumbing.Provenance{OwnerID: owner, Producer: plumbing.ProducerControlPlane, SourceRef: source, RecordedAt: at}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	records, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return records
}
