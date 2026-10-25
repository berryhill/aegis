package contribution

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/berryhill/aegis/internal/reference"
)

const ContinuitySchema = "aegis.contribution.continuity.v1"

var (
	ErrContinuityConflict  = errors.New("contribution continuity conflict")
	ErrContinuityFenced    = errors.New("contribution continuity fenced")
	ErrContinuityExhausted = errors.New("contribution continuity budget exhausted")
)

// Counters are independent cumulative reservations, never per-session allowances.
// Queue claims and runtime turns must reserve before their respective effects.
type Counters struct {
	Candidates             uint32 `json:"candidates"`
	QueueAttempts          uint32 `json:"queue_attempts"`
	RuntimeTurns           uint32 `json:"runtime_turns"`
	InfrastructureRechecks uint32 `json:"infrastructure_rechecks"`
	DecisionDeliveries     uint32 `json:"decision_deliveries"`
}

// ContinuityLimits is sealed by the owning service on first use. The candidate
// and Queue limits may not exceed the original admission. There is no reset API.
type ContinuityLimits = Counters

type ContinuityEvent struct {
	Continuation    *Continuation       `json:"continuation,omitempty"`
	OperationID     string              `json:"operation_id,omitempty"`
	NativeBoundary  *NativeBoundary     `json:"native_boundary,omitempty"`
	NativeEffect    *NativeEffect       `json:"native_effect,omitempty"`
	RoleStop        *RoleStop           `json:"role_stop,omitempty"`
	RoleAuthority   *RoleAuthority      `json:"role_authority,omitempty"`
	SchemaVersion   string              `json:"schema_version"`
	QueueItemID     string              `json:"queue_item_id"`
	ContractDigest  string              `json:"contract_digest"`
	OwnerID         string              `json:"owner_id"`
	SessionID       string              `json:"session_id"`
	Sequence        uint64              `json:"sequence"`
	PreviousDigest  string              `json:"previous_digest"`
	Kind            string              `json:"kind"`
	Stage           string              `json:"stage"`
	Candidate       string              `json:"candidate"`
	Evidence        reference.DigestRef `json:"evidence"`
	Counter         string              `json:"counter"`
	DecisionID      string              `json:"decision_id"`
	DecisionOutcome string              `json:"decision_outcome"`
	Limits          ContinuityLimits    `json:"limits"`
	At              time.Time           `json:"at"`
	Digest          string              `json:"digest"`
}

type Continuity struct {
	Continuation       *Continuation             `json:"continuation,omitempty"`
	Reservations       map[string]string         `json:"reservations,omitempty"`
	NativeBoundaries   map[string]NativeBoundary `json:"native_boundaries,omitempty"`
	NativeEffects      map[string]NativeEffect   `json:"native_effects,omitempty"`
	RoleEpochs         map[string]uint64         `json:"role_epochs,omitempty"`
	CandidateEvidence  reference.DigestRef       `json:"candidate_evidence"`
	RoleStops          map[string]RoleStop       `json:"role_stops,omitempty"`
	Observations       map[string]bool           `json:"observations,omitempty"`
	RoleAuthorities    map[string]RoleAuthority  `json:"role_authorities,omitempty"`
	QueueItemID        string                    `json:"queue_item_id"`
	ContractDigest     string                    `json:"contract_digest"`
	Sequence           uint64                    `json:"sequence"`
	Digest             string                    `json:"digest"`
	Limits             ContinuityLimits          `json:"limits"`
	Used               Counters                  `json:"used"`
	Stage              string                    `json:"stage"`
	Candidate          string                    `json:"candidate"`
	LastAt             time.Time                 `json:"last_at"`
	CancelAcknowledged bool                      `json:"cancel_acknowledged"`
	CancelRequested    bool                      `json:"cancel_requested"`
	Decisions          map[string]string         `json:"decisions"`
}

func ContinuityStage(stage string) bool {
	switch stage {
	case "resolve", "seal", "prepare", "build", "freeze", "test", "repair", "review", "publish", "checks", "merge", "dispose":
		return true
	}
	return false
}

func SealContinuityEvent(e ContinuityEvent) (ContinuityEvent, error) {
	e.Digest = ""
	if e.SchemaVersion != ContinuitySchema || e.QueueItemID == "" || e.ContractDigest == "" || e.OwnerID == "" || e.SessionID == "" || e.Sequence == 0 || e.At.IsZero() {
		return e, ErrContinuityConflict
	}
	b, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	e.Digest = Digest(b)
	return e, nil
}

func UnmarshalContinuityEvent(b []byte) (ContinuityEvent, error) {
	var e ContinuityEvent
	if reference.RejectDuplicateObjectKeys(b) != nil {
		return e, ErrContinuityConflict
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&e) != nil || d.Decode(new(any)) != io.EOF {
		return e, ErrContinuityConflict
	}
	sealed, err := SealContinuityEvent(e)
	canonical, _ := json.Marshal(e)
	if err != nil || sealed.Digest != e.Digest || !bytes.Equal(canonical, b) {
		return e, ErrContinuityConflict
	}
	return e, nil
}

func validContinuityPayload(e ContinuityEvent) bool {
	if e.Kind != "continuation-admitted" && e.Continuation != nil {
		return false
	}
	payload := e
	switch e.Kind {
	case "continuation-admitted":
		payload.Continuation = nil
	case "initialize":
		payload.Limits = Counters{}
	case "reserve":
		payload.Counter = ""
		payload.OperationID = ""
	case "checkpoint":
		payload.Stage = ""
		payload.Candidate = ""
		payload.Evidence = reference.DigestRef{}
	case "decision":
		payload.DecisionID = ""
		payload.DecisionOutcome = ""
	case "native-boundary-open", "native-boundary-extend", "native-boundary-closed":
		payload.NativeBoundary = nil
	case "native-reserved", "native-completed", "native-unknown":
		payload.NativeEffect = nil
	case "role-stop-requested", "role-stop-acknowledged":
		payload.RoleStop = nil
	case "role-admitted":
		payload.RoleAuthority = nil
	case "observation", "cancel-requested", "cancel-acknowledged":
	default:
		return false
	}
	return payload.OperationID == "" && payload.NativeBoundary == nil && payload.NativeEffect == nil && payload.RoleStop == nil && payload.RoleAuthority == nil && payload.Limits == (Counters{}) && payload.Counter == "" && payload.Stage == "" && payload.Candidate == "" && payload.Evidence == (reference.DigestRef{}) && payload.DecisionID == "" && payload.DecisionOutcome == ""
}

// ApplyContinuity reconstructs only retained control facts. Checkpoint evidence
// is a reference, not a verified stage verdict, authority grant or disposition.
// In particular this reducer cannot authorize an effect or acknowledge a stop.
func ApplyContinuity(c Contract, prior Continuity, e ContinuityEvent) (Continuity, error) {
	if _, err := Marshal(c); err != nil {
		return prior, ErrContinuityConflict
	}
	if (e.SessionID != c.SessionID && e.Kind != "cancel-requested" && e.Kind != "observation" && e.Kind != "role-admitted" && e.Kind != "role-stop-requested" && e.Kind != "role-stop-acknowledged" && e.Kind != "native-reserved" && e.Kind != "native-completed" && e.Kind != "native-unknown" && e.Kind != "native-boundary-open" && e.Kind != "native-boundary-extend" && e.Kind != "native-boundary-closed") || !validContinuityPayload(e) {
		return prior, ErrContinuityConflict
	}
	sealed, err := SealContinuityEvent(e)
	if err != nil || sealed.Digest != e.Digest || e.QueueItemID != c.QueueItemID || e.ContractDigest != c.Digest || e.OwnerID != c.OwnerID || e.Sequence != prior.Sequence+1 || e.PreviousDigest != prior.Digest || e.At.Before(c.AdmittedAt) || e.At.Before(prior.LastAt) {
		return prior, ErrContinuityConflict
	}
	// Expiry does not prevent preserving cancellation intent. It never renews the
	// original deadline, grants or runtime. Stop acknowledgement is separate.
	if e.Kind != "cancel-acknowledged" && e.Kind != "native-boundary-closed" && e.Kind != "native-completed" && e.Kind != "native-unknown" && e.Kind != "cancel-requested" && e.Kind != "role-stop-requested" && e.Kind != "role-stop-acknowledged" && !e.At.Before(c.Deadline) {
		return prior, ErrContinuityExhausted
	}
	if prior.CancelRequested && e.Kind != "cancel-acknowledged" && e.Kind != "native-boundary-closed" && e.Kind != "native-completed" && e.Kind != "native-unknown" && e.Kind != "role-stop-requested" && e.Kind != "role-stop-acknowledged" {
		return prior, ErrContinuityFenced
	}
	next := prior
	next.Decisions = make(map[string]string, len(prior.Decisions))
	for k, v := range prior.Decisions {
		next.Decisions[k] = v
	}
	if prior.Sequence == 0 && e.Kind == "cancel-requested" {
		if e.Limits != (Counters{}) {
			return prior, ErrContinuityConflict
		}
		next.QueueItemID = c.QueueItemID
		next.ContractDigest = c.Digest
		next.CancelRequested = true
	} else if prior.Sequence == 0 {
		if e.Kind != "initialize" || e.Limits.Candidates == 0 || e.Limits.Candidates > uint32(c.CandidateBudget) || e.Limits.QueueAttempts != 1 || e.Limits.RuntimeTurns == 0 || e.Limits.RuntimeTurns > 1024 || e.Limits.InfrastructureRechecks > 2 || e.Limits.DecisionDeliveries == 0 || e.Limits.DecisionDeliveries > 32 {
			return prior, ErrContinuityConflict
		}
		next.QueueItemID = c.QueueItemID
		next.ContractDigest = c.Digest
		next.Limits = e.Limits
	} else {
		if e.Limits != (Counters{}) {
			return prior, ErrContinuityConflict
		}
		switch e.Kind {
		case "continuation-admitted":
			if err := applyContinuation(c, prior, e, &next); err != nil {
				return prior, err
			}
		case "native-boundary-open", "native-boundary-extend", "native-boundary-closed":
			if err := applyNativeBoundary(c, prior, e, &next); err != nil {
				return prior, err
			}
		case "native-reserved", "native-completed", "native-unknown":
			if err := applyNativeEffect(c, prior, e, &next); err != nil {
				return prior, err
			}
		case "role-stop-requested", "role-stop-acknowledged":
			if err := applyRoleStop(prior, e, &next); err != nil {
				return prior, err
			}
		case "observation", "role-admitted":
			if err := applyRoleAuthority(c, prior, e, &next); err != nil {
				return prior, err
			}
		case "reserve":
			if len(e.OperationID) > 256 {
				return prior, ErrContinuityConflict
			}
			if e.OperationID != "" {
				next.Reservations = make(map[string]string, len(prior.Reservations)+1)
				for k, v := range prior.Reservations {
					next.Reservations[k] = v
				}
				if existing, ok := prior.Reservations[e.OperationID]; ok {
					if existing != e.Counter {
						return prior, ErrContinuityConflict
					}
					break
				}
				next.Reservations[e.OperationID] = e.Counter
			}
			var used *uint32
			var limit uint32
			switch e.Counter {
			case "candidate":
				used = &next.Used.Candidates
				limit = next.Limits.Candidates
			case "queue-attempt":
				used = &next.Used.QueueAttempts
				limit = next.Limits.QueueAttempts
			case "runtime-turn":
				used = &next.Used.RuntimeTurns
				limit = next.Limits.RuntimeTurns
			case "infrastructure-recheck":
				used = &next.Used.InfrastructureRechecks
				limit = next.Limits.InfrastructureRechecks
			case "decision-delivery":
				used = &next.Used.DecisionDeliveries
				limit = next.Limits.DecisionDeliveries
			default:
				return prior, ErrContinuityConflict
			}
			if *used >= limit {
				return prior, ErrContinuityExhausted
			}
			*used++
		case "checkpoint":
			if !ContinuityStage(e.Stage) || e.Evidence.Validate() != nil || len(e.Candidate) > 256 {
				return prior, ErrContinuityConflict
			}
			next.Stage = e.Stage
			next.Candidate = e.Candidate
			next.CandidateEvidence = e.Evidence
		case "decision":
			if !ValidKey(e.DecisionID) {
				return prior, ErrContinuityConflict
			}
			previous := next.Decisions[e.DecisionID]
			valid := false
			switch e.DecisionOutcome {
			case "requested":
				valid = previous == "" && next.Used.DecisionDeliveries > uint32(len(next.Decisions))
			case "delivered":
				valid = previous == "requested"
			case "unanswered":
				valid = previous == "delivered"
			case "denied", "expired", "dialog-cancelled":
				valid = previous == "delivered" || previous == "unanswered"
			case "unavailable":
				valid = previous == "requested"
			case "timeout", "unknown":
				valid = previous == "requested" || previous == "delivered"
			}
			// No caller-supplied approved state exists. Only the protected authority
			// service can issue an actual approval; observations cannot mint one.
			if !valid {
				return prior, ErrContinuityConflict
			}
			next.Decisions[e.DecisionID] = e.DecisionOutcome
		case "cancel-acknowledged":
			if !prior.CancelRequested || prior.CancelAcknowledged {
				return prior, ErrContinuityFenced
			}
			for role, r := range prior.RoleAuthorities {
				stop, ok := prior.RoleStops[role]
				if !ok || !stop.Acknowledged || stop.Authority != r {
					return prior, ErrNativeQuiescenceUnavailable
				}
			}
			for _, b := range prior.NativeBoundaries {
				if !b.Closed {
					return prior, ErrNativeQuiescenceUnavailable
				}
			}
			for _, n := range prior.NativeEffects {
				if n.State != "completed" {
					return prior, ErrNativeQuiescenceUnavailable
				}
			}
			next.CancelAcknowledged = true
		case "cancel-requested":
			next.CancelRequested = true
		default:
			return prior, ErrContinuityConflict
		}
	}
	next.Sequence = e.Sequence
	next.Digest = e.Digest
	next.LastAt = e.At
	return next, nil
}
