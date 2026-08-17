package app

import (
	"errors"
	"testing"

	"github.com/berryhill/aegis/internal/persistence/fleet"
)

func TestCollectionReadinessPreservesAuthoritativeEmptyAndReady(t *testing.T) {
	tests := []struct {
		name   string
		count  int
		state  string
		reason string
	}{
		{name: "empty", count: 0, state: "empty", reason: "collection_read_succeeded_empty"},
		{name: "ready", count: 3, state: "ready", reason: "collection_read_succeeded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectionReadiness(test.count, "fleet.test_records", nil)
			if got.State != test.state || got.ReasonCode != test.reason || got.Source != "fleet.test_records" || got.Count != test.count || !got.Authoritative {
				t.Fatalf("readiness=%+v", got)
			}
		})
	}
}

func TestCollectionReadinessFailureStatesNeverAssertAZeroCount(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		state  string
		reason string
	}{
		{name: "denied", err: ErrDenied, state: "denied", reason: "collection_read_denied"},
		{name: "unavailable", err: fleet.ErrClosed, state: "unavailable", reason: "collection_read_unavailable"},
		{name: "repair required", err: fleet.ErrCorrupt, state: "degraded_repair_required", reason: "fleet_store_corrupt"},
		{name: "error", err: errors.New("unexpected read failure"), state: "error", reason: "collection_read_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := collectionReadiness(9, "fleet.test_records", test.err)
			if got.State != test.state || got.ReasonCode != test.reason || got.Source != "fleet.test_records" || got.Count != 0 || got.Authoritative {
				t.Fatalf("failure readiness asserted collection facts: %+v", got)
			}
		})
	}
}
