package badger

import (
	"encoding/json"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	badgerdb "github.com/dgraph-io/badger/v4"
	"time"
)

// No external admission callback under the transaction. Current role admission
// is performed by the closed app verifier; every fleet-owned anchor is reloaded.
func validContributionCurrentCompletion(txn *badgerdb.Txn, r evidence.ContributionTerminalReport, claim queue.Claim, at time.Time) bool {
	p := r.Current
	if p == nil || r.Validate() != nil || p.OriginalAuthority != claim.Authority || r.Claim != claim.ClaimID || r.Attempt != claim.AttemptID {
		return false
	}
	c, e := loadContribution(txn, claim.QueueItem.ID)
	if e != nil || c.Digest != r.Contract || !at.Before(c.Deadline) {
		return false
	}
	state, _, e := loadContributionContinuity(txn, c.QueueItemID)
	if e != nil || state.Digest != r.Continuity || state.CancelRequested || state.Used.QueueAttempts != 1 || state.Used.Candidates > uint32(c.CandidateBudget) {
		return false
	}
	role, ok := state.RoleAuthorities["controller"]
	if !ok || role.Authority != p.Authority || role.Ref() != p.Role || role.Epoch != p.Epoch || role.MandateID != p.Mandate {
		return false
	}
	if _, stopped := state.RoleStops["controller"]; stopped {
		return false
	}
	lease := state.Continuation
	if lease == nil || lease.ClaimID != claim.ClaimID || lease.AttemptID != claim.AttemptID || lease.Controller != role.Ref() || lease.Epoch != role.Epoch || !at.Before(lease.ExpiresAt) || lease.ExpiresAt.After(c.Deadline) {
		return false
	}
	wire, _ := json.Marshal(lease)
	if p.Continuation != (reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: claim.ClaimID, Digest: contribution.Digest(wire)}) {
		return false
	}
	control, e := loadContributionControl(txn, c.QueueItemID)
	if e != nil || control.Stop {
		return false
	}
	scope := c.Effect
	if control.Sequence > 0 {
		scope, e = contribution.RestrictDelivery(c.Effect, control.Delivery)
		if e != nil {
			return false
		}
	}
	if scope != r.Scope {
		return false
	}
	bindingWire, e := get(txn, key(familyQueueRuntimeBinding, c.QueueItemID))
	if e != nil {
		return false
	}
	binding, e := queue.UnmarshalRuntimeBinding(bindingWire)
	return e == nil && binding.Authority == claim.Authority && binding.MandateID == p.OriginalMandate && role.OriginalRuntimeBinding == (reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: binding.BindingID, Digest: binding.Digest})
}
