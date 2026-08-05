package store

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

const (
	auditOutboxVersion = 1
	maxDeliveryBatch   = 1000
	terminalAttempts   = 3
)

type auditOutboxEntry struct {
	EventID       string     `json:"event_id"`
	EventDigest   string     `json:"event_digest"`
	Status        string     `json:"status"`
	Attempts      int        `json:"attempts"`
	LastReason    string     `json:"last_reason,omitempty"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
}

type auditOutbox struct {
	Version int                `json:"version"`
	Entries []auditOutboxEntry `json:"entries"`
}

func (s *Store) outboxPath() string     { return filepath.Join(s.root, "audit-outbox.json") }
func (s *Store) projectionPath() string { return filepath.Join(s.root, "audit-projection.jsonl") }

func loadAuditOutbox(path string) (auditOutbox, error) {
	var out auditOutbox
	err := read(path, &out)
	if errors.Is(err, os.ErrNotExist) {
		return auditOutbox{Version: auditOutboxVersion, Entries: []auditOutboxEntry{}}, nil
	}
	if err != nil {
		return out, err
	}
	if out.Version != auditOutboxVersion {
		return out, errors.New("unsupported audit outbox version")
	}
	return out, nil
}

func reconcileAuditOutbox(events []core.AuditEvent, out auditOutbox) (auditOutbox, error) {
	if out.Version != auditOutboxVersion || len(out.Entries) > len(events) {
		return out, errors.New("invalid audit outbox")
	}
	for i := range out.Entries {
		entry := out.Entries[i]
		if entry.EventID != events[i].ID || entry.EventDigest != events[i].EventDigest {
			return out, errors.New("audit outbox does not match canonical ordering")
		}
		switch entry.Status {
		case "pending", "delivered", "retryable_failure", "terminal_failure":
		default:
			return out, errors.New("audit outbox contains invalid status")
		}
	}
	for i := len(out.Entries); i < len(events); i++ {
		out.Entries = append(out.Entries, auditOutboxEntry{EventID: events[i].ID, EventDigest: events[i].EventDigest, Status: "pending"})
	}
	return out, nil
}

func readAuditProjection(path string) ([]core.AuditEvent, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []core.AuditEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []core.AuditEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var event core.AuditEvent
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, scanner.Err()
}

func projectionPrefix(events, projected []core.AuditEvent) error {
	if len(projected) > len(events) {
		return errors.New("audit projection is ahead of canonical audit")
	}
	for i := range projected {
		if projected[i].ID != events[i].ID || projected[i].EventDigest != events[i].EventDigest {
			return errors.New("audit projection does not match canonical ordering")
		}
	}
	return nil
}

func appendProjection(path string, event core.AuditEvent) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(encoded, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func auditDeliveryStatus(events, projected []core.AuditEvent, out auditOutbox) core.AuditDeliveryStatus {
	status := core.AuditDeliveryStatus{State: "pending", Reason: "audit_delivery_pending", CanonicalEvents: len(events), ProjectedEvents: len(projected), Verifiable: true}
	for _, entry := range out.Entries {
		switch entry.Status {
		case "pending":
			status.Pending++
		case "retryable_failure":
			status.Pending++
			status.RetryableFailures++
		case "terminal_failure":
			status.TerminalFailures++
		}
	}
	if status.TerminalFailures > 0 {
		status.State, status.Reason = "degraded", "audit_delivery_terminal_failure"
	} else if status.RetryableFailures > 0 {
		status.State, status.Reason = "degraded", "audit_delivery_retryable_failure"
	} else if len(events) == len(projected) && status.Pending == 0 {
		status.State, status.Reason, status.Current = "healthy", "audit_delivery_current", true
	}
	return status
}

func unverifiableAuditStatus(reason string) core.AuditDeliveryStatus {
	return core.AuditDeliveryStatus{State: "unverifiable", Reason: reason, Verifiable: false}
}

// AuditDeliveryStatus verifies the canonical chain and reports only bounded,
// sanitized aggregate delivery evidence. Missing derived state is lag, not
// proof of loss, because it can be rebuilt from the canonical chain.
func (s *Store) AuditDeliveryStatus() core.AuditDeliveryStatus {
	if err := s.VerifyAudit(); err != nil {
		return unverifiableAuditStatus("audit_chain_unverifiable")
	}
	events, err := s.AuditEvents()
	if err != nil {
		return unverifiableAuditStatus("audit_chain_unverifiable")
	}
	projected, err := readAuditProjection(s.projectionPath())
	if err != nil || projectionPrefix(events, projected) != nil {
		return unverifiableAuditStatus("audit_projection_unverifiable")
	}
	out, err := loadAuditOutbox(s.outboxPath())
	if err != nil {
		return unverifiableAuditStatus("audit_outbox_unverifiable")
	}
	out, err = reconcileAuditOutbox(events, out)
	if err != nil {
		return unverifiableAuditStatus("audit_outbox_unverifiable")
	}
	return auditDeliveryStatus(events, projected, out)
}

// DeliverAudit drains at most limit records in canonical order into the local
// durable projection. Projection-first/outbox-second publication is restart
// safe: a retry recognizes an already projected digest and cannot duplicate it.
func (s *Store) DeliverAudit(ctx context.Context, limit int) (core.AuditDeliveryResult, error) {
	if limit == 0 {
		limit = 100
	}
	if limit < 0 || limit > maxDeliveryBatch {
		return core.AuditDeliveryResult{}, errors.New("audit delivery batch exceeds limit")
	}
	result := core.AuditDeliveryResult{}
	err := s.withLock(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.VerifyAudit(); err != nil {
			return fmt.Errorf("canonical audit verification failed: %w", err)
		}
		events, err := s.AuditEvents()
		if err != nil {
			return err
		}
		projected, err := readAuditProjection(s.projectionPath())
		if err != nil || projectionPrefix(events, projected) != nil {
			return errors.New("audit projection is unverifiable")
		}
		out, err := loadAuditOutbox(s.outboxPath())
		if err != nil {
			return err
		}
		out, err = reconcileAuditOutbox(events, out)
		if err != nil {
			return err
		}
		// Repair a crash after projection publication but before outbox status.
		for i := range projected {
			if out.Entries[i].Status != "delivered" {
				now := time.Now().UTC()
				out.Entries[i].Status, out.Entries[i].LastReason, out.Entries[i].DeliveredAt = "delivered", "", &now
			}
		}
		if err = writeAtomic(s.outboxPath(), out); err != nil {
			return err
		}
		for len(projected) < len(events) && result.Delivered < limit {
			if err = ctx.Err(); err != nil {
				return err
			}
			index := len(projected)
			entry := &out.Entries[index]
			if entry.Status == "terminal_failure" {
				break
			}
			now := time.Now().UTC()
			entry.Attempts++
			entry.LastAttemptAt = &now
			if err = appendProjection(s.projectionPath(), events[index]); err != nil {
				entry.Status, entry.LastReason = "retryable_failure", "projection_write_failed"
				if entry.Attempts >= terminalAttempts {
					entry.Status, entry.LastReason = "terminal_failure", "projection_retry_limit_reached"
				}
				if persistErr := writeAtomic(s.outboxPath(), out); persistErr != nil {
					return errors.Join(err, persistErr)
				}
				return err
			}
			projected = append(projected, events[index])
			entry.Status, entry.LastReason, entry.DeliveredAt = "delivered", "", &now
			if err = writeAtomic(s.outboxPath(), out); err != nil {
				return err
			}
			result.Delivered++
		}
		return nil
	})
	result.Status = s.AuditDeliveryStatus()
	return result, err
}

func (s *Store) VerifyAuditProjection() error {
	status := s.AuditDeliveryStatus()
	if !status.Verifiable {
		return fmt.Errorf("audit delivery verification failed: %s", status.Reason)
	}
	return nil
}

// RebuildAuditProjection replaces only derived delivery state after verifying
// the canonical chain. It never modifies canonical events or checkpoints.
func (s *Store) RebuildAuditProjection(ctx context.Context) (core.AuditDeliveryStatus, error) {
	err := s.withLock(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.VerifyAudit(); err != nil {
			return fmt.Errorf("canonical audit verification failed: %w", err)
		}
		events, err := s.AuditEvents()
		if err != nil {
			return err
		}
		var payload []byte
		out := auditOutbox{Version: auditOutboxVersion, Entries: make([]auditOutboxEntry, 0, len(events))}
		now := time.Now().UTC()
		for _, event := range events {
			encoded, marshalErr := json.Marshal(event)
			if marshalErr != nil {
				return marshalErr
			}
			payload = append(payload, append(encoded, '\n')...)
			out.Entries = append(out.Entries, auditOutboxEntry{EventID: event.ID, EventDigest: event.EventDigest, Status: "delivered", DeliveredAt: &now})
		}
		if err = writeBytesAtomic(s.projectionPath(), payload); err != nil {
			return err
		}
		return writeAtomic(s.outboxPath(), out)
	})
	return s.AuditDeliveryStatus(), err
}

func writeBytesAtomic(path string, payload []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".aegis-projection-*")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(payload)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
