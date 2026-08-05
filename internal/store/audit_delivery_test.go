package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/core"
)

func appendAuditEvents(t *testing.T, s *Store, count int) []core.AuditEvent {
	t.Helper()
	for i := 0; i < count; i++ {
		if err := s.AppendAudit(context.Background(), core.AuditEvent{Type: "delivery_test", Outcome: "ok", Reason: "focused_test"}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.AuditEvents()
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestAuditDeliveryIsBoundedOrderedAndRestartSafe(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	events := appendAuditEvents(t, s, 3)
	status := s.AuditDeliveryStatus()
	if status.State != "pending" || status.Pending != 3 || status.CanonicalEvents != 3 || status.ProjectedEvents != 0 || !status.Verifiable || status.Current {
		t.Fatalf("initial delivery status = %+v", status)
	}

	result, err := s.DeliverAudit(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if result.Delivered != 2 || result.Status.Pending != 1 || result.Status.ProjectedEvents != 2 || result.Status.Current {
		t.Fatalf("bounded delivery result = %+v", result)
	}
	projected, err := readAuditProjection(s.projectionPath())
	if err != nil {
		t.Fatal(err)
	}
	for i := range projected {
		if projected[i].ID != events[i].ID || projected[i].EventDigest != events[i].EventDigest {
			t.Fatalf("projection[%d] is out of canonical order", i)
		}
	}

	reopened, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.AuditDeliveryStatus(); got.Pending != 1 || got.ProjectedEvents != 2 {
		t.Fatalf("reopened status lost progress = %+v", got)
	}
	result, err = reopened.DeliverAudit(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Delivered != 1 || result.Status.State != "healthy" || !result.Status.Current || result.Status.Pending != 0 {
		t.Fatalf("final delivery result = %+v", result)
	}
	if err = reopened.VerifyAuditProjection(); err != nil {
		t.Fatal(err)
	}
}

func TestAuditDeliveryRepairsProjectionFirstInterruptionWithoutDuplication(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	events := appendAuditEvents(t, s, 2)
	if err = appendProjection(s.projectionPath(), events[0]); err != nil {
		t.Fatal(err)
	}

	result, err := s.DeliverAudit(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Delivered != 1 || !result.Status.Current {
		t.Fatalf("interruption recovery result = %+v", result)
	}
	projected, err := readAuditProjection(s.projectionPath())
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 2 || projected[0].ID != events[0].ID || projected[1].ID != events[1].ID {
		t.Fatalf("recovered projection = %+v", projected)
	}
	out, err := loadAuditOutbox(s.outboxPath())
	if err != nil {
		t.Fatal(err)
	}
	if out.Entries[0].Status != "delivered" || out.Entries[1].Status != "delivered" {
		t.Fatalf("reconciled outbox = %+v", out)
	}
}

func TestAuditDeliveryFailsClosedAndRebuildsOnlyDerivedState(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	canonical := appendAuditEvents(t, s, 2)
	if _, err = s.DeliverAudit(context.Background(), 100); err != nil {
		t.Fatal(err)
	}

	projected := append([]core.AuditEvent(nil), canonical...)
	projected[0].EventDigest = "tampered-projection-digest"
	var payload []byte
	for _, event := range projected {
		encoded, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		payload = append(payload, append(encoded, '\n')...)
	}
	if err = os.WriteFile(filepath.Join(s.Root(), "audit-projection.jsonl"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	status := s.AuditDeliveryStatus()
	if status.State != "unverifiable" || status.Reason != "audit_projection_unverifiable" || status.Verifiable || status.Current {
		t.Fatalf("tampered projection status = %+v", status)
	}
	if _, err = s.DeliverAudit(context.Background(), 100); err == nil {
		t.Fatal("delivery accepted an unverifiable projection")
	}
	if err = s.VerifyAuditProjection(); err == nil {
		t.Fatal("verification accepted an unverifiable projection")
	}

	status, err = s.RebuildAuditProjection(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "healthy" || !status.Current || !status.Verifiable {
		t.Fatalf("rebuilt status = %+v", status)
	}
	after, err := s.AuditEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(canonical) {
		t.Fatalf("rebuild changed canonical count: before=%d after=%d", len(canonical), len(after))
	}
	for i := range after {
		if after[i].ID != canonical[i].ID || after[i].EventDigest != canonical[i].EventDigest {
			t.Fatalf("rebuild changed canonical event %d", i)
		}
	}
}

func TestAuditDeliveryRejectsInvalidBatchAndOutboxMetadata(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	appendAuditEvents(t, s, 1)
	for _, limit := range []int{-1, maxDeliveryBatch + 1} {
		if _, err = s.DeliverAudit(context.Background(), limit); err == nil {
			t.Fatalf("delivery accepted invalid limit %d", limit)
		}
	}
	if err = os.WriteFile(s.outboxPath(), []byte(`{"version":1,"entries":[{"event_id":"wrong","event_digest":"wrong","status":"delivered","attempts":0}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	status := s.AuditDeliveryStatus()
	if status.State != "unverifiable" || status.Reason != "audit_outbox_unverifiable" || status.Verifiable {
		t.Fatalf("mismatched outbox status = %+v", status)
	}
	if _, err = s.DeliverAudit(context.Background(), 100); err == nil {
		t.Fatal("delivery accepted mismatched outbox metadata")
	}
}
