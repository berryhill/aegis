package contribution

import "time"

// Decision is a retained exact provisioning intent, never an authority grant.
// Unknown/applying is non-replayable; reconcile its canonical approval/receipt.
type Decision struct {
	ID             string      `json:"id"`
	Key            string      `json:"key"`
	OwnerID        string      `json:"owner_id"`
	QueueItemID    string      `json:"queue_item_id"`
	ContractDigest string      `json:"contract_digest"`
	ControlDigest  string      `json:"control_digest"`
	Role           string      `json:"role"`
	Binding        RoleBinding `json:"binding"`
	Stage          string      `json:"stage"`
	Candidate      string      `json:"candidate"`
	PlanID         string      `json:"plan_id"`
	PlanDigest     string      `json:"plan_digest"`
	RequestedAt    time.Time   `json:"requested_at"`
	ExpiresAt      time.Time   `json:"expires_at"`
	Status         string      `json:"status"`
	ApprovalID     string      `json:"approval_id,omitempty"`
	ReceiptID      string      `json:"receipt_id,omitempty"`
}

func DecisionTransition(from, to string) bool {
	switch from {
	case "requested":
		return to == "delivered" || to == "unavailable" || to == "timeout" || to == "unknown" || to == "expired" || to == "dialog-cancelled"
	case "delivered":
		return to == "applying" || to == "denied" || to == "dialog-cancelled" || to == "unanswered" || to == "expired" || to == "unknown"
	case "applying":
		return to == "approved" || to == "unknown" || to == "applying"
	}
	return false
}
