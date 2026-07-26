// Package poc implements the narrow participant-to-verification lifecycle proof.
// It composes existing Aegis authority, Hermes attempt, and create-only storage
// primitives; it does not infer identity, select a stanza, or issue authority.
package poc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/plumbing"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
)

const (
	aggregateRecordKind = "poc-aggregates"
	artifactRecordKind  = "poc-artifacts"
	evidenceRecordKind  = "poc-evidence"
)

var (
	ErrDenied          = errors.New("POC execution denied")
	ErrInvalidReadback = errors.New("POC readback is invalid")
)

// AttemptRunner is deliberately one-turn shaped. The POC cannot ask a runtime
// to retry, select another stanza, or create a second session implicitly.
type AttemptRunner interface {
	AttemptTurn(context.Context, hermes.AttemptTurnRequest) (hermes.AttemptTurnResult, error)
}

// ArtifactVerifier is a separate Aegis-controlled component. It receives only
// persisted artifact metadata and cannot trust the runtime response in memory.
type ArtifactVerifier interface {
	Verify(context.Context, plumbing.Artifact, string) (plumbing.VerificationEvidence, error)
}

type Service struct {
	attempts AttemptRunner
	verifier ArtifactVerifier
	store    *store.Store
}

func New(attempts AttemptRunner, verifier ArtifactVerifier, records *store.Store) (*Service, error) {
	if attempts == nil || verifier == nil || records == nil {
		return nil, errors.New("POC service dependencies are required")
	}
	return &Service{attempts: attempts, verifier: verifier, store: records}, nil
}

// RunInput contains operation data, not authentication or stanza-selection
// hints. Aggregate must already contain one authenticated participant, one
// allowed exactly-one-stanza decision, and an unexpired Hermes authority.
type RunInput struct {
	Aggregate       plumbing.Aggregate
	ParentAttemptID string
	StateRoot       string
	Prompt          string
	Expected        string
	OperationType   string
	OperationSchema string
	Destination     string
	Model           string
	Provider        string
	Credentials     []hermes.Credential
	Bounds          hermes.AttemptBounds
}

// Run executes exactly one Hermes turn, persists its output by digest, obtains
// verification from the distinct verifier, and publishes a terminal aggregate.
func (s *Service) Run(ctx context.Context, input RunInput) (plumbing.Aggregate, error) {
	if err := validateInput(input, time.Now().UTC()); err != nil {
		return plumbing.Aggregate{}, err
	}

	attemptID := store.ID("attempt")
	result, attemptErr := s.attempts.AttemptTurn(ctx, hermes.AttemptTurnRequest{
		Aggregate:       input.Aggregate,
		AttemptID:       attemptID,
		ParentAttemptID: input.ParentAttemptID,
		StateRoot:       input.StateRoot,
		Input:           input.Prompt,
		Model:           input.Model,
		Provider:        input.Provider,
		Credentials:     input.Credentials,
		Bounds:          input.Bounds,
	})
	if result.Attempt.ID == "" {
		// A denial or setup failure before Hermes starts creates no lifecycle fact
		// and cannot be converted into model-backed authority by this service.
		return plumbing.Aggregate{}, attemptErr
	}

	aggregate := input.Aggregate
	aggregate.Revision++
	aggregate.Attempts = append(aggregate.Attempts, result.Attempt)
	if result.Attempt.FinishedAt == nil {
		return plumbing.Aggregate{}, errors.New("Hermes returned a non-terminal attempt")
	}
	factAt := *result.Attempt.FinishedAt

	if attemptErr != nil || result.Attempt.State != plumbing.AttemptSucceeded {
		state, reason := attemptDisposition(result.Attempt.State)
		aggregate.Disposition = disposition(aggregate.OwnerID, state, reason, nil, factAt)
		aggregate.UpdatedAt = factAt
		if err := s.persist(ctx, aggregate, nil, nil); err != nil {
			return plumbing.Aggregate{}, errors.Join(attemptErr, err)
		}
		if attemptErr == nil {
			attemptErr = errors.New("Hermes attempt did not succeed")
		}
		return aggregate, attemptErr
	}

	controlProvenance := provenance(aggregate.OwnerID, plumbing.ProducerControlPlane, "poc:"+aggregate.ID, factAt)
	promptDigest := digest([]byte(input.Prompt))
	operation := plumbing.Operation{
		ID: store.ID("operation"), AuthorityContextID: aggregate.Authority.ID,
		SessionAttemptID: result.Attempt.ID, Type: input.OperationType,
		SchemaVersion: input.OperationSchema, ParametersDigest: promptDigest,
		CreatedAt: factAt, Provenance: controlProvenance,
	}
	request := plumbing.Request{
		ID: store.ID("request"), OperationID: operation.ID,
		PayloadDigest: promptDigest, Deadline: aggregate.Authority.ExpiresAt,
		CreatedAt: factAt, Provenance: controlProvenance,
	}
	outputRef, err := s.store.PutBlob([]byte(result.Output))
	if err != nil {
		return plumbing.Aggregate{}, err
	}
	artifact := plumbing.Artifact{
		ID: store.ID("artifact"), RequestID: request.ID, OwnerID: aggregate.OwnerID,
		Kind: "hermes-turn-output", Revision: 1,
		Digest: strings.TrimPrefix(outputRef, "sha256:"), ContentRef: outputRef,
		MediaType: "text/plain; charset=utf-8", CreatedAt: factAt,
		Provenance: provenance(aggregate.OwnerID, plumbing.ProducerRuntimeAdapter, "hermes-attempt:"+result.Attempt.RuntimeAttemptID, factAt),
	}

	evidence, err := s.verifier.Verify(ctx, artifact, digest([]byte(input.Expected)))
	if err != nil {
		return plumbing.Aggregate{}, err
	}
	if err = validateVerifierResult(aggregate, artifact, evidence); err != nil {
		return plumbing.Aggregate{}, err
	}

	finishedAt := evidence.ObservedAt
	delivery := plumbing.Delivery{
		ID: store.ID("delivery"), RequestID: request.ID, ArtifactID: artifact.ID,
		AuthorityContextID: aggregate.Authority.ID, Destination: input.Destination,
		State: plumbing.DeliveryDelivered, AttemptedAt: factAt, FinishedAt: &finishedAt,
		Reason: "artifact_persisted", Provenance: provenance(aggregate.OwnerID, plumbing.ProducerControlPlane, outputRef, finishedAt),
	}
	aggregate.Operations = append(aggregate.Operations, operation)
	aggregate.Requests = append(aggregate.Requests, request)
	aggregate.Artifacts = append(aggregate.Artifacts, artifact)
	aggregate.Deliveries = append(aggregate.Deliveries, delivery)
	aggregate.Evidence = append(aggregate.Evidence, evidence)
	state, reason := plumbing.DispositionSucceeded, "artifact_verified"
	if evidence.Outcome != plumbing.VerificationPassed {
		state, reason = plumbing.DispositionFailed, "artifact_verification_failed"
	}
	aggregate.Disposition = disposition(aggregate.OwnerID, state, reason, []string{evidence.ID}, finishedAt)
	aggregate.UpdatedAt = finishedAt
	if err = s.persist(ctx, aggregate, &artifact, &evidence); err != nil {
		return plumbing.Aggregate{}, err
	}
	if state != plumbing.DispositionSucceeded {
		return aggregate, errors.New("artifact verification failed")
	}
	return aggregate, nil
}

// Read reconstructs one terminal lifecycle from create-only records. A record
// is returned only after the aggregate, separately persisted artifact and
// evidence records, and the authoritative audit chain all agree.
func (s *Service) Read(ctx context.Context, id string) (plumbing.Aggregate, error) {
	if err := ctx.Err(); err != nil {
		return plumbing.Aggregate{}, err
	}
	if strings.TrimSpace(id) == "" {
		return plumbing.Aggregate{}, fmt.Errorf("%w: aggregate ID is required", ErrInvalidReadback)
	}
	var matches []plumbing.Aggregate
	err := s.store.List(aggregateRecordKind, func(raw json.RawMessage) error {
		var aggregate plumbing.Aggregate
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&aggregate); err != nil {
			return err
		}
		if aggregate.ID == id {
			matches = append(matches, aggregate)
		}
		return nil
	})
	if err != nil {
		return plumbing.Aggregate{}, fmt.Errorf("%w: %v", ErrInvalidReadback, err)
	}
	if len(matches) != 1 {
		return plumbing.Aggregate{}, fmt.Errorf("%w: expected one terminal aggregate, found %d", ErrInvalidReadback, len(matches))
	}
	aggregate := matches[0]
	if aggregate.Disposition == nil {
		return plumbing.Aggregate{}, fmt.Errorf("%w: aggregate is not terminal", ErrInvalidReadback)
	}
	if err := plumbing.Validate(aggregate); err != nil {
		return plumbing.Aggregate{}, fmt.Errorf("%w: aggregate: %v", ErrInvalidReadback, err)
	}
	for _, artifact := range aggregate.Artifacts {
		var stored plumbing.Artifact
		if err := s.store.Load(artifactRecordKind, artifact.ID, &stored); err != nil || stored != artifact {
			return plumbing.Aggregate{}, fmt.Errorf("%w: artifact %q is missing or mismatched", ErrInvalidReadback, artifact.ID)
		}
	}
	for _, evidence := range aggregate.Evidence {
		var stored plumbing.VerificationEvidence
		if err := s.store.Load(evidenceRecordKind, evidence.ID, &stored); err != nil || stored != evidence {
			return plumbing.Aggregate{}, fmt.Errorf("%w: evidence %q is missing or mismatched", ErrInvalidReadback, evidence.ID)
		}
	}
	if err := s.store.VerifyAudit(); err != nil {
		return plumbing.Aggregate{}, fmt.Errorf("%w: audit chain: %v", ErrInvalidReadback, err)
	}
	events, err := s.store.AuditEvents()
	if err != nil {
		return plumbing.Aggregate{}, fmt.Errorf("%w: audit events: %v", ErrInvalidReadback, err)
	}
	auditMatches := 0
	for _, event := range events {
		if event.Type == "poc.lifecycle.terminal" && event.Metadata["aggregate_id"] == aggregate.ID && event.Metadata["revision"] == fmt.Sprint(aggregate.Revision) && event.Metadata["disposition_id"] == aggregate.Disposition.ID && event.Outcome == string(aggregate.Disposition.State) && event.Reason == aggregate.Disposition.Reason {
			auditMatches++
		}
	}
	if auditMatches != 1 {
		return plumbing.Aggregate{}, fmt.Errorf("%w: expected one matching terminal audit event, found %d", ErrInvalidReadback, auditMatches)
	}
	return aggregate, nil
}

func validateInput(input RunInput, now time.Time) error {
	if err := plumbing.Validate(input.Aggregate); err != nil {
		return fmt.Errorf("%w: %v", ErrDenied, err)
	}
	if input.Aggregate.Authority == nil || input.Aggregate.Disposition != nil ||
		!input.Aggregate.Decision.Allowed || input.Aggregate.Decision.MatchingCount != 1 ||
		input.Aggregate.Decision.SelectedStanzaID == "" {
		return fmt.Errorf("%w: exactly one active authority context is required", ErrDenied)
	}
	authority := input.Aggregate.Authority
	if authority.Runtime != "hermes-agent" || authority.Digest != plumbing.AuthorityDigest(*authority) ||
		now.Before(authority.IssuedAt) || !now.Before(authority.ExpiresAt) {
		return fmt.Errorf("%w: authority is invalid, expired, or selects another runtime", ErrDenied)
	}
	for _, revocation := range input.Aggregate.Revocations {
		if revocation.AuthorityContextID == authority.ID && !now.Before(revocation.RevokedAt) {
			return fmt.Errorf("%w: authority is revoked", ErrDenied)
		}
	}
	parentFound := false
	for _, attempt := range input.Aggregate.Attempts {
		parentFound = parentFound || (attempt.ID == input.ParentAttemptID && attempt.Kind == plumbing.AttemptDispatch && attempt.State == plumbing.AttemptSucceeded && attempt.AuthorityContextID == authority.ID)
	}
	if !parentFound {
		return fmt.Errorf("%w: successful parent dispatch is missing or mismatched", ErrDenied)
	}
	if strings.TrimSpace(input.OperationType) == "" || strings.TrimSpace(input.OperationSchema) == "" || strings.TrimSpace(input.Destination) == "" {
		return errors.New("operation type, schema, and destination are required")
	}
	return nil
}

func validateVerifierResult(aggregate plumbing.Aggregate, artifact plumbing.Artifact, evidence plumbing.VerificationEvidence) error {
	if evidence.ID == "" || evidence.SubjectKind != "artifact" || evidence.SubjectID != artifact.ID ||
		evidence.Verifier != plumbing.VerifierArtifact || evidence.Provenance.Producer != plumbing.ProducerVerifier ||
		evidence.Provenance.OwnerID != aggregate.OwnerID || evidence.ObservedAt.Before(artifact.CreatedAt) ||
		(evidence.Outcome != plumbing.VerificationPassed && evidence.Outcome != plumbing.VerificationFailed) {
		return errors.New("distinct verifier returned invalid or mismatched evidence")
	}
	return nil
}

func (s *Service) persist(ctx context.Context, aggregate plumbing.Aggregate, artifact *plumbing.Artifact, evidence *plumbing.VerificationEvidence) error {
	if err := plumbing.Validate(aggregate); err != nil {
		return err
	}
	if artifact != nil {
		if err := s.store.Create(artifactRecordKind, artifact.ID, artifact); err != nil {
			return err
		}
	}
	if evidence != nil {
		if err := s.store.Create(evidenceRecordKind, evidence.ID, evidence); err != nil {
			return err
		}
	}
	recordID := fmt.Sprintf("%s-r%d", aggregate.ID, aggregate.Revision)
	if err := s.store.Create(aggregateRecordKind, recordID, aggregate); err != nil {
		return err
	}
	return s.store.AppendAudit(ctx, core.AuditEvent{
		Type: "poc.lifecycle.terminal", Outcome: string(aggregate.Disposition.State), Reason: aggregate.Disposition.Reason,
		Metadata: map[string]string{"aggregate_id": aggregate.ID, "revision": fmt.Sprint(aggregate.Revision), "disposition_id": aggregate.Disposition.ID},
	})
}

func attemptDisposition(state plumbing.AttemptState) (plumbing.DispositionState, string) {
	switch state {
	case plumbing.AttemptDenied:
		return plumbing.DispositionDenied, "hermes_attempt_denied"
	case plumbing.AttemptCancelled:
		return plumbing.DispositionCancelled, "hermes_attempt_cancelled"
	case plumbing.AttemptExpired:
		return plumbing.DispositionExpired, "hermes_attempt_expired"
	default:
		return plumbing.DispositionFailed, "hermes_attempt_failed"
	}
}

func disposition(owner string, state plumbing.DispositionState, reason string, evidence []string, at time.Time) *plumbing.TerminalDisposition {
	return &plumbing.TerminalDisposition{ID: store.ID("disposition"), State: state, Reason: reason, EvidenceIDs: evidence, DecidedAt: at, Provenance: provenance(owner, plumbing.ProducerControlPlane, "poc-disposition", at)}
}

func provenance(owner string, producer plumbing.ProvenanceProducer, source string, at time.Time) plumbing.Provenance {
	return plumbing.Provenance{OwnerID: owner, Producer: producer, SourceRef: source, RecordedAt: at}
}

func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
