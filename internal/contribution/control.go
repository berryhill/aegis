package contribution

import "time"

// Control is authenticated by the application and appended with contract-bound CAS.
// Upstream issue content can never create a Control.
type Control struct {
	QueueItemID    string    `json:"queue_item_id"`
	ContractDigest string    `json:"contract_digest"`
	OwnerID        string    `json:"owner_id"`
	SessionID      string    `json:"session_id"`
	Sequence       uint64    `json:"sequence"`
	PreviousDigest string    `json:"previous_digest"`
	Digest         string    `json:"digest"`
	Stop           bool      `json:"stop"`
	Delivery       string    `json:"delivery"`
	At             time.Time `json:"at"`
}
