package contribution

import (
	"fmt"
	"regexp"
)

var deliveryReadinessID = regexp.MustCompile(`^readiness/[0-9a-f]{64}/[1-2]$`)

// applyDeliveryReadiness records accounting only. The owning Service must verify
// the external canonical UNKNOWN intent and repeat current effect admission.
func applyDeliveryReadiness(prior Continuity, e ContinuityEvent, next *Continuity) error {
	if prior.Sequence == 0 {
		return ErrContinuityConflict
	}
	if prior.CancelRequested || prior.CancelAcknowledged {
		return ErrContinuityFenced
	}
	r, ok := prior.RoleAuthorities["controller"]
	if !ok || e.RoleAuthority == nil || *e.RoleAuthority != r || r.Role != "controller" || r.Epoch == 0 || r.Epoch != prior.RoleEpochs["controller"] || e.SessionID != r.SessionID {
		return ErrContinuityConflict
	}
	if _, stopped := prior.RoleStops["controller"]; stopped {
		return ErrContinuityFenced
	}
	if e.Counter != "infrastructure-recheck" || (e.Stage != "checks" && e.Stage != "merge") || e.Candidate == "" || e.Candidate != prior.Candidate || e.Evidence != prior.CandidateEvidence || e.Evidence.Validate() != nil {
		return ErrContinuityConflict
	}
	if !deliveryReadinessID.MatchString(e.OperationID) || e.OperationID[len(e.OperationID)-1:] != fmt.Sprint(prior.Used.InfrastructureRechecks+1) {
		return ErrContinuityConflict
	}
	if _, duplicate := prior.Reservations[e.OperationID]; duplicate {
		return ErrContinuityConflict
	}
	if prior.Used.InfrastructureRechecks >= prior.Limits.InfrastructureRechecks || prior.Used.InfrastructureRechecks >= 2 {
		return ErrContinuityExhausted
	}
	next.Reservations = make(map[string]string, len(prior.Reservations)+1)
	for k, v := range prior.Reservations {
		next.Reservations[k] = v
	}
	next.Reservations[e.OperationID] = e.Counter
	next.Used.InfrastructureRechecks++
	return nil
}
