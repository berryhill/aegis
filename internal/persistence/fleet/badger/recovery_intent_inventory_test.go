package badger

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/queue"
)

// These fixtures use normal persistence writers, not handcrafted canonical rows.
// A prepared or rejected intent legitimately has no Queue item/Contribution.
func TestRecoveryPreparedRejectedAndHistoricalIntents(t *testing.T) {
	for _, state := range []string{"prepared", "rejected", "historical-accepted"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			s, a := lifecycleFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion), "recovery-intent")
			defer s.Close()
			switch state {
			case "rejected":
				r, err := queue.NewRejection(queue.Rejection{RejectionID: "recovery-rejected", SubmissionID: a.Submission.SubmissionID, IdempotencyKey: a.Submission.IdempotencyKey, ReasonCode: "authority_denied", Reason: "fresh authority admission denied", RejectedAt: a.Submission.SubmittedAt})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.RejectSubmission(ctx, r, audit("submission.rejected", r.RejectionID)); err != nil {
					t.Fatal(err)
				}
				if got, err := s.GetRejection(ctx, r.RejectionID); err != nil || got != r {
					t.Fatalf("rejection readback: %v", err)
				}
			case "historical-accepted":
				if _, err := s.AcceptSubmission(ctx, a, audit("submission.accepted", a.Submission.SubmissionID)); err != nil {
					t.Fatal(err)
				}
				assertAcceptedReadback(t, s, a)
			}
			before := recoveryInventory(t, s)
			if _, _, err := verifyFleetRecovery(ctx, s.db); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, recoveryInventory(t, s)) {
				t.Fatal("verification mutated canonical state")
			}
		})
	}
}

func TestRecoveryReportedKeyDecode(t *testing.T) {
	const reported = "027100097072696e636970616c000d6c6f6f70712d66697874757265" // synthetic loopq-fixture
	raw, err := hex.DecodeString(reported)
	if err != nil {
		t.Fatal(err)
	}
	parts, err := decodeKeyParts(raw, familyQueueArchiveHead)
	want := []string{"principal", "loopq-fixture"}
	if err != nil || !reflect.DeepEqual(parts, want) {
		t.Fatalf("key decode: %q %v", parts, err)
	}
	if familyQueueArchiveHead != familyContributionRequest {
		t.Fatal("update legacy collision inventory after an explicit migration")
	}
	t.Logf("namespace=%x family=%x parts=%q; key alone cannot establish record meaning", raw[0], raw[1], parts)
}
