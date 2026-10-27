package store

import (
	"crypto/ed25519"
	"time"

	"github.com/berryhill/aegis/internal/hostapproval"
)

// ReadHistoricalDoerHostApproval verifies a completed Run's signed fact at its
// recorded evidence time. It is not live host consent and must never be used by
// admission, preparation or runtime effects. Later generations remain verified.
func (s *Store) ReadHistoricalDoerHostApproval(deployment, owner string, agent hostapproval.AgentReference, contract hostapproval.Contract, runID string, recordedAt time.Time) (hostapproval.Record, error) {
	var result hostapproval.Record
	if runID == "" || recordedAt.IsZero() {
		return result, hostapproval.ErrDenied
	}
	err := s.withLock(func() error {
		key, err := s.hostApprovalKey(false)
		if err != nil {
			return err
		}
		history, err := s.doerHostApprovalHistory(deployment, owner, agent, contract, key)
		if err != nil {
			return err
		}
		matches := 0
		for _, record := range history {
			if record.RunRequestID != runID || recordedAt.Before(record.ApprovedAt) || !recordedAt.Before(record.ExpiresAt) {
				continue
			}
			if err := hostapproval.Verify(record, key.Public().(ed25519.PublicKey), deployment, owner, agent, contract, recordedAt); err != nil {
				return err
			}
			result = record
			matches++
		}
		if matches != 1 {
			return hostapproval.ErrDenied
		}
		return nil
	})
	return result, err
}
