package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/berryhill/aegis/internal/loop"
)

// ContributionDeliveryRepair links an independently admitted, immutable remote
// failure to the accepted native pass it invalidated. It grants no remote rights.
// Failure and Evidence are canonical intent/envelope digests, not remote prose.
type ContributionDeliveryRepair struct {
	Failure   string                      `json:"failure"`
	Evidence  string                      `json:"evidence"`
	Candidate string                      `json:"candidate"`
	Class     string                      `json:"class"`
	Head      string                      `json:"head"`
	Base      string                      `json:"base"`
	PR        uint64                      `json:"pr"`
	AfterPass uint8                       `json:"after_pass"`
	Feedback  ContributionFailureFeedback `json:"feedback"`
}

func deliveryRepairKey(run string, after uint8) []byte {
	return []byte(fmt.Sprintf("contribution/delivery-repair/%s/%d", digest([]byte(run)), after))
}

func validDeliveryRepair(r ContributionDeliveryRepair, pass ContributionPass) bool {
	return r.Feedback.Valid(r.Class) && r.AfterPass == pass.Candidate.Pass && r.Candidate == pass.Candidate.Digest &&
		pass.Check.Outcome == "passed" && sha256Digest(r.Failure) && sha256Digest(r.Evidence) &&
		len(r.Head) == 40 && len(r.Base) == 40 && r.PR != 0 &&
		(r.Class == "ci_failed" || r.Class == "merge_conflict")
}

func sha256Digest(s string) bool {
	if len(s) != 71 || s[:7] != "sha256:" {
		return false
	}
	for _, c := range s[7:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// admitDeliveryRepair is called ONLY under the same workspace lock as Run.
// The controller callback repeats current role/control/deadline and exact remote
// history admission here, after delivery's accepted-source frame has ended.
func (e *ManagedContribution) admitDeliveryRepair(ctx context.Context, r ContributionRecord, c loop.VerifiedImplementation) (ContributionRecord, error) {
	bad := errors.New("exact productive delivery repair admission required")
	if ctx == nil || ctx.Err() != nil || e.RepairAdmission == nil || r.State != "candidate_accepted" || r.Frozen != nil ||
		len(r.Passes) == 0 || int(r.Consumed) != len(r.Passes) || r.Consumed >= c.MaxPasses {
		return r, bad
	}
	pass := r.Passes[len(r.Passes)-1]
	// Validate the unchanged source, original policy/toolchain and accepted pass
	// before asking the owner to authorize its invalidation.
	boundary := r
	boundary.State = "phase_boundary"
	if err := e.resumeBoundary(ctx, boundary, r.RunID, r.Contract, r.QueueAttempt, r.Base, c, r.Policy); err != nil {
		return r, err
	}
	repair, err := e.RepairAdmission(ctx, r)
	if err != nil {
		return r, err
	}
	if !validDeliveryRepair(repair, pass) {
		return r, bad
	}
	for _, old := range r.DeliveryRepairs {
		if old.AfterPass >= repair.AfterPass || old.Failure == repair.Failure {
			return r, bad
		}
	}
	if err = e.admit(ctx, "delivery-repair"); err != nil {
		return r, err
	}
	wire, _ := json.Marshal(repair)
	key := deliveryRepairKey(r.RunID, repair.AfterPass)
	// An interruption between immutable append and progress save must replay the
	// SAME admission; it must never allocate another pass or replace the failure.
	if old, readErr := e.DB.Get(key); readErr == nil {
		if !bytes.Equal(old, wire) {
			return r, bad
		}
	} else if !errors.Is(readErr, ErrNotFound) {
		return r, readErr
	} else if err = e.DB.Create(key, wire); err != nil {
		return r, err
	}
	r.DeliveryRepairs = append(r.DeliveryRepairs, repair)
	r.State = "phase_boundary"
	// Consumption remains in runPrepared, persisted before the next builder effect.
	return r, e.save(r)
}

func (e *ManagedContribution) validateDeliveryRepairs(r ContributionRecord) error {
	var after uint8
	seen := map[string]bool{}
	for _, repair := range r.DeliveryRepairs {
		if repair.AfterPass == 0 || repair.AfterPass <= after || int(repair.AfterPass) > len(r.Passes) || seen[repair.Failure] ||
			!validDeliveryRepair(repair, r.Passes[int(repair.AfterPass)-1]) {
			return errors.New("delivery repair lineage mismatch")
		}
		wire, _ := json.Marshal(repair)
		stored, err := e.DB.Get(deliveryRepairKey(r.RunID, repair.AfterPass))
		if err != nil || !bytes.Equal(wire, stored) {
			return errors.New("delivery repair lineage mismatch")
		}
		after = repair.AfterPass
		seen[repair.Failure] = true
	}
	return nil
}
