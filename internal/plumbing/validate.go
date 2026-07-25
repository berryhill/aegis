package plumbing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

var ErrInvalid = errors.New("invalid plumbing aggregate")

func AuthorityDigest(authority AuthorityContext) string {
	authority.Digest = ""
	encoded, err := json.Marshal(authority)
	if err != nil {
		panic(err) // AuthorityContext contains only JSON-safe concrete values.
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func CanTransitionAttempt(from, to AttemptState) bool {
	switch from {
	case AttemptRequested:
		return to == AttemptStarted || to == AttemptDenied || to == AttemptCancelled || to == AttemptExpired
	case AttemptStarted:
		return to == AttemptSucceeded || to == AttemptFailed || to == AttemptCancelled || to == AttemptExpired
	default:
		return false
	}
}

func CanTransitionDelivery(from, to DeliveryState) bool {
	return from == DeliveryPending && (to == DeliveryDelivered || to == DeliveryFailed || to == DeliveryDenied || to == DeliveryCancelled)
}

func Validate(aggregate Aggregate) error {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	if aggregate.SchemaVersion != SchemaVersion {
		return invalid("unsupported schema version %q", aggregate.SchemaVersion)
	}
	if err := validID("aggregate.id", aggregate.ID); err != nil {
		return invalid("%v", err)
	}
	if aggregate.Revision == 0 {
		return invalid("revision must be positive")
	}
	if err := validID("aggregate.owner_id", aggregate.OwnerID); err != nil {
		return invalid("%v", err)
	}
	if aggregate.CreatedAt.IsZero() || aggregate.UpdatedAt.Before(aggregate.CreatedAt) {
		return invalid("aggregate timestamps are missing or reversed")
	}
	checkFactTime := func(kind string, at time.Time) error {
		if at.IsZero() || at.Before(aggregate.CreatedAt) || at.After(aggregate.UpdatedAt) {
			return fmt.Errorf("%s timestamp is outside the aggregate lifetime", kind)
		}
		return nil
	}

	ids := map[string]string{}
	addID := func(kind, id string) error {
		if err := validID(kind+".id", id); err != nil {
			return err
		}
		if previous, exists := ids[id]; exists {
			return fmt.Errorf("id %q is shared by %s and %s", id, previous, kind)
		}
		ids[id] = kind
		return nil
	}
	checkProvenance := func(kind string, factAt time.Time, provenance Provenance) error {
		if provenance.OwnerID != aggregate.OwnerID {
			return fmt.Errorf("%s provenance owner %q does not match aggregate owner", kind, provenance.OwnerID)
		}
		if !validProvenanceProducer(provenance.Producer) || strings.TrimSpace(provenance.SourceRef) == "" || provenance.RecordedAt.IsZero() {
			return fmt.Errorf("%s provenance is incomplete or has an untrusted producer", kind)
		}
		if provenance.RecordedAt.Before(aggregate.CreatedAt) || provenance.RecordedAt.After(aggregate.UpdatedAt) {
			return fmt.Errorf("%s provenance timestamp is outside the aggregate lifetime", kind)
		}
		if provenance.RecordedAt.Before(factAt) {
			return fmt.Errorf("%s provenance predates the fact it records", kind)
		}
		return nil
	}
	subjectTimes := map[string]time.Time{}

	participant := aggregate.Participant
	if err := addID("participant", participant.ID); err != nil {
		return invalid("%v", err)
	}
	if strings.TrimSpace(participant.Kind) == "" {
		return invalid("participant kind is required")
	}
	if err := checkProvenance("participant", participant.Authentication.AuthenticatedAt, participant.Provenance); err != nil {
		return invalid("%v", err)
	}
	auth := participant.Authentication
	if err := validID("participant.authentication.evidence_id", auth.EvidenceID); err != nil {
		return invalid("%v", err)
	}
	if auth.Issuer == "" || auth.Method == "" || auth.AuthenticatedAt.IsZero() || !auth.ExpiresAt.After(auth.AuthenticatedAt) {
		return invalid("participant authentication is incomplete or expired at issuance")
	}
	if err := validDigest("participant.authentication.claims_digest", auth.ClaimsDigest); err != nil {
		return invalid("%v", err)
	}
	if err := checkFactTime("participant authentication", auth.AuthenticatedAt); err != nil {
		return invalid("%v", err)
	}
	subjectTimes[participant.ID] = auth.AuthenticatedAt

	ingress := aggregate.Ingress
	if err := addID("ingress", ingress.ID); err != nil {
		return invalid("%v", err)
	}
	if ingress.ParticipantID != participant.ID {
		return invalid("ingress participant does not match authenticated participant")
	}
	for name, value := range map[string]string{"contact_id": ingress.ContactID, "channel_id": ingress.ChannelID, "channel_kind": ingress.ChannelKind, "endpoint_ref": ingress.EndpointRef} {
		if strings.TrimSpace(value) == "" {
			return invalid("ingress %s is required", name)
		}
	}
	if ingress.ObservedAt.IsZero() || ingress.ObservedAt.Before(auth.AuthenticatedAt) || !ingress.ObservedAt.Before(auth.ExpiresAt) {
		return invalid("ingress observation is outside authenticated lifetime")
	}
	if err := checkFactTime("ingress observation", ingress.ObservedAt); err != nil {
		return invalid("%v", err)
	}
	if err := checkProvenance("ingress", ingress.ObservedAt, ingress.Provenance); err != nil {
		return invalid("%v", err)
	}
	subjectTimes[ingress.ID] = ingress.ObservedAt

	decision := aggregate.Decision
	if err := addID("decision", decision.ID); err != nil {
		return invalid("%v", err)
	}
	if decision.ParticipantID != participant.ID || decision.IngressFactID != ingress.ID {
		return invalid("decision causal references do not match participant and ingress")
	}
	if decision.AgentID == "" || decision.CharterRevision == 0 || decision.Reason == "" || decision.DecidedAt.IsZero() {
		return invalid("decision identity, revision, reason, and timestamp are required")
	}
	if decision.DecidedAt.Before(ingress.ObservedAt) || !decision.DecidedAt.Before(auth.ExpiresAt) {
		return invalid("decision timestamp is outside its causal authentication window")
	}
	if err := checkFactTime("decision", decision.DecidedAt); err != nil {
		return invalid("%v", err)
	}
	if err := validDigest("decision.charter_digest", decision.CharterDigest); err != nil {
		return invalid("%v", err)
	}
	if decision.Allowed {
		if decision.MatchingCount != 1 || decision.SelectedStanzaID == "" {
			return invalid("allowed decision requires exactly one selected stanza")
		}
	} else if decision.MatchingCount == 1 || decision.SelectedStanzaID != "" {
		return invalid("denied decision must not expose a selected stanza")
	}
	if err := checkProvenance("decision", decision.DecidedAt, decision.Provenance); err != nil {
		return invalid("%v", err)
	}
	subjectTimes[decision.ID] = decision.DecidedAt

	if !decision.Allowed {
		if aggregate.Authority != nil || len(aggregate.Revocations)+len(aggregate.Attempts)+len(aggregate.Operations)+len(aggregate.Requests)+len(aggregate.Artifacts)+len(aggregate.Deliveries) != 0 {
			return invalid("denied decision cannot issue authority or create downstream work")
		}
		if aggregate.Disposition == nil || aggregate.Disposition.State != DispositionDenied {
			return invalid("denied decision requires a denied terminal disposition")
		}
	}

	authorityID := ""
	var authority AuthorityContext
	if decision.Allowed {
		if aggregate.Authority == nil {
			return invalid("allowed decision requires an authority context")
		}
		authority = *aggregate.Authority
		if err := addID("authority", authority.ID); err != nil {
			return invalid("%v", err)
		}
		authorityID = authority.ID
		if authority.MandateID == "" || authority.DecisionID != decision.ID || authority.ParticipantID != participant.ID || authority.AgentID != decision.AgentID || authority.StanzaID != decision.SelectedStanzaID || authority.CharterRevision != decision.CharterRevision || authority.CharterDigest != decision.CharterDigest {
			return invalid("authority context does not exactly bind the mandate decision")
		}
		if authority.Runtime == "" || authority.RuntimeVersion == "" || authority.IssuedAt.Before(decision.DecidedAt) || !authority.IssuedAt.Before(auth.ExpiresAt) || !authority.ExpiresAt.After(authority.IssuedAt) || authority.ExpiresAt.After(auth.ExpiresAt) {
			return invalid("authority runtime or lifetime is invalid")
		}
		if err := checkFactTime("authority issuance", authority.IssuedAt); err != nil {
			return invalid("%v", err)
		}
		for name, values := range map[string][]string{"capabilities": authority.Capabilities, "tools": authority.Tools, "memory_scopes": authority.MemoryScopes, "credential_scopes": authority.CredentialScopes} {
			if err := sortedUnique(name, values); err != nil {
				return invalid("%v", err)
			}
		}
		if authority.Digest != AuthorityDigest(authority) {
			return invalid("authority digest does not bind the immutable context")
		}
		if err := checkProvenance("authority", authority.IssuedAt, authority.Provenance); err != nil {
			return invalid("%v", err)
		}
		subjectTimes[authority.ID] = authority.IssuedAt
	}
	authorityCutoff := authority.ExpiresAt
	var previousRevocationAt time.Time
	for i, revocation := range aggregate.Revocations {
		kind := fmt.Sprintf("revocations[%d]", i)
		if err := addID(kind, revocation.ID); err != nil {
			return invalid("%v", err)
		}
		if revocation.AuthorityContextID != authorityID || revocation.Reason == "" || revocation.RevokedAt.Before(authority.IssuedAt) || !revocation.RevokedAt.Before(authority.ExpiresAt) {
			return invalid("%s has broken authority, reason, or timestamp binding", kind)
		}
		if !previousRevocationAt.IsZero() && revocation.RevokedAt.Before(previousRevocationAt) {
			return invalid("%s is chronologically out of order", kind)
		}
		previousRevocationAt = revocation.RevokedAt
		if err := checkFactTime(kind, revocation.RevokedAt); err != nil {
			return invalid("%v", err)
		}
		if revocation.RevokedAt.Before(authorityCutoff) {
			authorityCutoff = revocation.RevokedAt
		}
		if err := checkProvenance(kind, revocation.RevokedAt, revocation.Provenance); err != nil {
			return invalid("%v", err)
		}
		subjectTimes[revocation.ID] = revocation.RevokedAt
	}

	attempts := map[string]Attempt{}
	for i, attempt := range aggregate.Attempts {
		kind := fmt.Sprintf("attempts[%d]", i)
		if err := addID(kind, attempt.ID); err != nil {
			return invalid("%v", err)
		}
		if attempt.AuthorityContextID != authorityID {
			return invalid("%s authority context does not match", kind)
		}
		if attempt.Kind != AttemptDispatch && attempt.Kind != AttemptSession {
			return invalid("%s has unknown kind %q", kind, attempt.Kind)
		}
		if !validAttemptState(attempt.State) || attempt.RequestedAt.IsZero() {
			return invalid("%s has invalid state or timestamp", kind)
		}
		if attempt.RequestedAt.Before(authority.IssuedAt) || !attempt.RequestedAt.Before(authorityCutoff) {
			return invalid("%s was requested outside the authority lifetime", kind)
		}
		if err := validateAttemptTimes(attempt); err != nil {
			return invalid("%s: %v", kind, err)
		}
		if err := checkFactTime(kind+" request", attempt.RequestedAt); err != nil {
			return invalid("%v", err)
		}
		if attempt.StartedAt != nil {
			if !attempt.StartedAt.Before(authorityCutoff) {
				return invalid("%s started after authority ceased to be effective", kind)
			}
			if err := checkFactTime(kind+" start", *attempt.StartedAt); err != nil {
				return invalid("%v", err)
			}
		}
		if attempt.FinishedAt != nil {
			if attempt.State == AttemptSucceeded && !attempt.FinishedAt.Before(authorityCutoff) {
				return invalid("%s succeeded after authority ceased to be effective", kind)
			}
			if err := checkFactTime(kind+" finish", *attempt.FinishedAt); err != nil {
				return invalid("%v", err)
			}
		}
		if attempt.Kind == AttemptDispatch && attempt.ParentAttemptID != "" {
			return invalid("dispatch attempt cannot have a parent attempt")
		}
		if attempt.Kind == AttemptSession {
			parent, ok := attempts[attempt.ParentAttemptID]
			if !ok || parent.Kind != AttemptDispatch || parent.State != AttemptSucceeded {
				return invalid("session attempt requires an earlier successful dispatch attempt")
			}
			if parent.FinishedAt == nil || attempt.RequestedAt.Before(*parent.FinishedAt) {
				return invalid("session attempt predates its dispatch completion")
			}
		}
		attemptFactAt := attempt.RequestedAt
		if attempt.StartedAt != nil {
			attemptFactAt = *attempt.StartedAt
		}
		if attempt.FinishedAt != nil {
			attemptFactAt = *attempt.FinishedAt
		}
		if err := checkProvenance(kind, attemptFactAt, attempt.Provenance); err != nil {
			return invalid("%v", err)
		}
		subjectTimes[attempt.ID] = attemptFactAt
		attempts[attempt.ID] = attempt
	}

	operations := map[string]Operation{}
	for i, operation := range aggregate.Operations {
		kind := fmt.Sprintf("operations[%d]", i)
		if err := addID(kind, operation.ID); err != nil {
			return invalid("%v", err)
		}
		session, ok := attempts[operation.SessionAttemptID]
		if operation.AuthorityContextID != authorityID || !ok || session.Kind != AttemptSession || session.StartedAt == nil {
			return invalid("%s is not bound to a started session authority", kind)
		}
		if operation.Type == "" || operation.SchemaVersion == "" || operation.CreatedAt.Before(authority.IssuedAt) || !operation.CreatedAt.Before(authorityCutoff) {
			return invalid("%s typed operation fields are incomplete", kind)
		}
		if session.StartedAt == nil || operation.CreatedAt.Before(*session.StartedAt) || (session.FinishedAt != nil && operation.CreatedAt.After(*session.FinishedAt)) {
			return invalid("%s timestamp is outside its session", kind)
		}
		if err := checkFactTime(kind, operation.CreatedAt); err != nil {
			return invalid("%v", err)
		}
		if err := validDigest(kind+".parameters_digest", operation.ParametersDigest); err != nil {
			return invalid("%v", err)
		}
		if err := checkProvenance(kind, operation.CreatedAt, operation.Provenance); err != nil {
			return invalid("%v", err)
		}
		subjectTimes[operation.ID] = operation.CreatedAt
		operations[operation.ID] = operation
	}

	requests := map[string]Request{}
	for i, request := range aggregate.Requests {
		kind := fmt.Sprintf("requests[%d]", i)
		if err := addID(kind, request.ID); err != nil {
			return invalid("%v", err)
		}
		operation, ok := operations[request.OperationID]
		if !ok {
			return invalid("%s references unknown operation", kind)
		}
		if request.ParentRequestID != "" {
			parent, ok := requests[request.ParentRequestID]
			if !ok {
				return invalid("%s parent request is not earlier in the aggregate", kind)
			}
			if request.CreatedAt.Before(parent.CreatedAt) {
				return invalid("%s predates its parent request", kind)
			}
		}
		if request.CreatedAt.Before(operation.CreatedAt) || !request.CreatedAt.Before(authorityCutoff) || !request.Deadline.After(request.CreatedAt) || request.Deadline.After(authority.ExpiresAt) {
			return invalid("%s deadline is invalid", kind)
		}
		if err := checkFactTime(kind, request.CreatedAt); err != nil {
			return invalid("%v", err)
		}
		if err := validDigest(kind+".payload_digest", request.PayloadDigest); err != nil {
			return invalid("%v", err)
		}
		if err := checkProvenance(kind, request.CreatedAt, request.Provenance); err != nil {
			return invalid("%v", err)
		}
		subjectTimes[request.ID] = request.CreatedAt
		requests[request.ID] = request
	}

	artifacts := map[string]Artifact{}
	for i, artifact := range aggregate.Artifacts {
		kind := fmt.Sprintf("artifacts[%d]", i)
		if err := addID(kind, artifact.ID); err != nil {
			return invalid("%v", err)
		}
		request, ok := requests[artifact.RequestID]
		if !ok || artifact.OwnerID == "" || artifact.Kind == "" || artifact.Revision == 0 || artifact.ContentRef == "" || artifact.MediaType == "" || artifact.CreatedAt.IsZero() {
			return invalid("%s identity, ownership, reference, or revision is invalid", kind)
		}
		if artifact.CreatedAt.Before(request.CreatedAt) || !artifact.CreatedAt.Before(request.Deadline) || !artifact.CreatedAt.Before(authorityCutoff) {
			return invalid("%s was created outside its causal authority window", kind)
		}
		if err := checkFactTime(kind, artifact.CreatedAt); err != nil {
			return invalid("%v", err)
		}
		if err := validDigest(kind+".digest", artifact.Digest); err != nil {
			return invalid("%v", err)
		}
		if err := checkProvenance(kind, artifact.CreatedAt, artifact.Provenance); err != nil {
			return invalid("%v", err)
		}
		subjectTimes[artifact.ID] = artifact.CreatedAt
		artifacts[artifact.ID] = artifact
	}

	deliveredArtifacts := map[string]bool{}
	for i, delivery := range aggregate.Deliveries {
		kind := fmt.Sprintf("deliveries[%d]", i)
		if err := addID(kind, delivery.ID); err != nil {
			return invalid("%v", err)
		}
		artifact, artifactOK := artifacts[delivery.ArtifactID]
		request, requestOK := requests[delivery.RequestID]
		if !requestOK || !artifactOK || artifact.RequestID != delivery.RequestID || delivery.AuthorityContextID != authorityID || delivery.Destination == "" || delivery.AttemptedAt.IsZero() {
			return invalid("%s has broken request, artifact, authority, or destination binding", kind)
		}
		if delivery.AttemptedAt.Before(artifact.CreatedAt) || !delivery.AttemptedAt.Before(request.Deadline) || !delivery.AttemptedAt.Before(authorityCutoff) {
			return invalid("%s was attempted outside its causal authority window", kind)
		}
		if err := validateDeliveryTimes(delivery); err != nil {
			return invalid("%s: %v", kind, err)
		}
		if err := checkFactTime(kind+" attempt", delivery.AttemptedAt); err != nil {
			return invalid("%v", err)
		}
		if delivery.FinishedAt != nil {
			if delivery.State == DeliveryDelivered && (!delivery.FinishedAt.Before(authorityCutoff) || !delivery.FinishedAt.Before(request.Deadline)) {
				return invalid("%s delivered after authority or request ceased to be effective", kind)
			}
			if err := checkFactTime(kind+" finish", *delivery.FinishedAt); err != nil {
				return invalid("%v", err)
			}
		}
		if delivery.State == DeliveryDelivered {
			deliveredArtifacts[delivery.ArtifactID] = true
		}
		deliveryFactAt := delivery.AttemptedAt
		if delivery.FinishedAt != nil {
			deliveryFactAt = *delivery.FinishedAt
		}
		if err := checkProvenance(kind, deliveryFactAt, delivery.Provenance); err != nil {
			return invalid("%v", err)
		}
		subjectTimes[delivery.ID] = deliveryFactAt
	}

	evidence := map[string]VerificationEvidence{}
	for i, item := range aggregate.Evidence {
		kind := fmt.Sprintf("evidence[%d]", i)
		if err := addID(kind, item.ID); err != nil {
			return invalid("%v", err)
		}
		if item.SubjectKind == "" || item.SubjectID == "" || !subjectKindMatches(item.SubjectKind, ids[item.SubjectID]) || !validVerifierID(item.Verifier) || item.EvidenceRef == "" || item.ObservedAt.IsZero() || (item.Outcome != VerificationPassed && item.Outcome != VerificationFailed) {
			return invalid("%s is incomplete or references an unknown subject", kind)
		}
		if err := checkFactTime(kind, item.ObservedAt); err != nil {
			return invalid("%v", err)
		}
		if subjectAt, ok := subjectTimes[item.SubjectID]; !ok || item.ObservedAt.Before(subjectAt) {
			return invalid("%s predates the subject it verifies", kind)
		}
		if err := validDigest(kind+".digest", item.Digest); err != nil {
			return invalid("%v", err)
		}
		if err := checkProvenance(kind, item.ObservedAt, item.Provenance); err != nil {
			return invalid("%v", err)
		}
		subjectTimes[item.ID] = item.ObservedAt
		evidence[item.ID] = item
	}
	if decision.Allowed && aggregate.Disposition == nil && !aggregate.UpdatedAt.Before(authorityCutoff) {
		return invalid("active aggregate outlives its effective authority")
	}

	if aggregate.Disposition != nil {
		disposition := *aggregate.Disposition
		if err := addID("disposition", disposition.ID); err != nil {
			return invalid("%v", err)
		}
		if !validDisposition(disposition.State) || disposition.Reason == "" || disposition.DecidedAt.IsZero() {
			return invalid("terminal disposition is incomplete")
		}
		if disposition.DecidedAt.Before(decision.DecidedAt) {
			return invalid("terminal disposition predates the stanza decision")
		}
		if err := checkFactTime("terminal disposition", disposition.DecidedAt); err != nil {
			return invalid("%v", err)
		}

		sessionSucceeded := false
		failed, denied, cancelled := false, !decision.Allowed, false
		expired := decision.Allowed && !disposition.DecidedAt.Before(authority.ExpiresAt)
		for _, attempt := range aggregate.Attempts {
			if attempt.FinishedAt == nil || attempt.State == AttemptRequested || attempt.State == AttemptStarted {
				return invalid("terminal disposition requires every attempt to be terminal")
			}
			if attempt.FinishedAt.After(disposition.DecidedAt) {
				return invalid("terminal disposition predates attempt %q", attempt.ID)
			}
			sessionSucceeded = sessionSucceeded || (attempt.Kind == AttemptSession && attempt.State == AttemptSucceeded)
			failed = failed || attempt.State == AttemptFailed
			denied = denied || attempt.State == AttemptDenied
			cancelled = cancelled || attempt.State == AttemptCancelled
			expired = expired || attempt.State == AttemptExpired
		}
		for _, delivery := range aggregate.Deliveries {
			if delivery.FinishedAt == nil || delivery.State == DeliveryPending {
				return invalid("terminal disposition requires every delivery to be terminal")
			}
			if delivery.FinishedAt.After(disposition.DecidedAt) {
				return invalid("terminal disposition predates delivery %q", delivery.ID)
			}
			failed = failed || delivery.State == DeliveryFailed
			denied = denied || delivery.State == DeliveryDenied
			cancelled = cancelled || delivery.State == DeliveryCancelled
		}

		seen := map[string]bool{}
		passed := false
		for _, evidenceID := range disposition.EvidenceIDs {
			if seen[evidenceID] || evidence[evidenceID].ID == "" {
				return invalid("terminal disposition has duplicate or unknown evidence %q", evidenceID)
			}
			if evidence[evidenceID].ObservedAt.After(disposition.DecidedAt) {
				return invalid("terminal disposition predates evidence %q", evidenceID)
			}
			passed = passed || evidence[evidenceID].Outcome == VerificationPassed
			failed = failed || evidence[evidenceID].Outcome == VerificationFailed
			seen[evidenceID] = true
		}
		if disposition.State == DispositionSucceeded {
			if !disposition.DecidedAt.Before(authorityCutoff) {
				return invalid("successful disposition follows authority expiry or revocation")
			}
			if !passed {
				return invalid("successful disposition requires passed verification evidence")
			}
			if !sessionSucceeded {
				return invalid("successful disposition requires a successful terminal session")
			}
			for artifactID := range artifacts {
				if !deliveredArtifacts[artifactID] {
					return invalid("successful disposition requires delivery of artifact %q", artifactID)
				}
			}
		} else {
			consistent := map[DispositionState]bool{
				DispositionFailed:    failed,
				DispositionDenied:    denied,
				DispositionCancelled: cancelled,
				DispositionExpired:   expired,
			}[disposition.State]
			if !consistent {
				return invalid("terminal disposition %q has no consistent terminal event", disposition.State)
			}
		}
		for _, item := range aggregate.Evidence {
			if item.ObservedAt.After(disposition.DecidedAt) {
				return invalid("terminal disposition predates evidence %q", item.ID)
			}
		}
		if err := checkProvenance("disposition", disposition.DecidedAt, disposition.Provenance); err != nil {
			return invalid("%v", err)
		}
		subjectTimes[disposition.ID] = disposition.DecidedAt
	}
	return nil
}

func validID(name, value string) error {
	if value == "" || len(value) > 255 || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be a non-empty bounded identifier", name)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains a control character", name)
		}
	}
	return nil
}

func validDigest(name, value string) error {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size || strings.ToLower(value) != value {
		return fmt.Errorf("%s must be a lowercase SHA-256 digest", name)
	}
	return nil
}

func sortedUnique(name string, values []string) error {
	if !sort.StringsAreSorted(values) {
		return fmt.Errorf("authority %s must be sorted", name)
	}
	for i, value := range values {
		if strings.TrimSpace(value) == "" || (i > 0 && values[i-1] == value) {
			return fmt.Errorf("authority %s must contain unique non-empty values", name)
		}
	}
	return nil
}

func validAttemptState(state AttemptState) bool {
	switch state {
	case AttemptRequested, AttemptStarted, AttemptSucceeded, AttemptFailed, AttemptDenied, AttemptCancelled, AttemptExpired:
		return true
	default:
		return false
	}
}

func validateAttemptTimes(attempt Attempt) error {
	switch attempt.State {
	case AttemptRequested:
		if attempt.StartedAt != nil || attempt.FinishedAt != nil {
			return errors.New("requested attempt cannot have start or finish time")
		}
	case AttemptStarted:
		if attempt.StartedAt == nil || attempt.StartedAt.Before(attempt.RequestedAt) || attempt.FinishedAt != nil {
			return errors.New("started attempt requires a valid start and no finish")
		}
	default:
		if attempt.FinishedAt == nil || attempt.FinishedAt.Before(attempt.RequestedAt) {
			return errors.New("terminal attempt requires a valid finish")
		}
		if attempt.State == AttemptSucceeded || attempt.State == AttemptFailed {
			if attempt.StartedAt == nil || attempt.StartedAt.Before(attempt.RequestedAt) || attempt.FinishedAt.Before(*attempt.StartedAt) {
				return errors.New("executed terminal attempt requires ordered start and finish")
			}
		}
		if attempt.State == AttemptDenied && attempt.StartedAt != nil {
			return errors.New("denied attempt cannot have a start time")
		}
		if attempt.StartedAt != nil && (attempt.StartedAt.Before(attempt.RequestedAt) || attempt.FinishedAt.Before(*attempt.StartedAt)) {
			return errors.New("terminal attempt timestamps are reversed")
		}
	}
	return nil
}

func subjectKindMatches(subjectKind, storedKind string) bool {
	switch subjectKind {
	case "participant", "ingress", "decision", "authority", "disposition":
		return storedKind == subjectKind
	case "attempt", "operation", "request", "artifact", "delivery", "revocation":
		return strings.HasPrefix(storedKind, subjectKind+"s[")
	default:
		return false
	}
}

func validateDeliveryTimes(delivery Delivery) error {
	switch delivery.State {
	case DeliveryPending:
		if delivery.FinishedAt != nil {
			return errors.New("pending delivery cannot have finish time")
		}
	case DeliveryDelivered, DeliveryFailed, DeliveryDenied, DeliveryCancelled:
		if delivery.FinishedAt == nil || delivery.FinishedAt.Before(delivery.AttemptedAt) {
			return errors.New("terminal delivery requires ordered finish time")
		}
	default:
		return fmt.Errorf("unknown delivery state %q", delivery.State)
	}
	return nil
}

func validDisposition(state DispositionState) bool {
	switch state {
	case DispositionSucceeded, DispositionFailed, DispositionDenied, DispositionCancelled, DispositionExpired:
		return true
	default:
		return false
	}
}

func validProvenanceProducer(producer ProvenanceProducer) bool {
	switch producer {
	case ProducerControlPlane, ProducerRuntimeAdapter, ProducerVerifier:
		return true
	default:
		return false
	}
}

func validVerifierID(verifier VerifierID) bool {
	return verifier == VerifierArtifact
}
