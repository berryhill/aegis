package badger

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/persistence/fleet"
)

func TestContributionCompletionRejectsGenericProofAtomically(t *testing.T) {
	ctx := context.Background()
	s, c, blobs := completionFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion))
	defer s.Close()
	before, err := s.GetQueueProjection(ctx, c.Claim.QueueItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.GetClaim(ctx, c.Claim.ClaimID)
	if err != nil {
		t.Fatal(err)
	}
	auditBefore, err := s.AuditEvents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteContributionQueueItem(ctx, c, completionAudit(c), blobs); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("generic provenance accepted: %v", err)
	}
	c.Provenance = evidence.CompletionProvenance{}
	if err = s.CompleteContributionQueueItem(ctx, c, completionAudit(c), blobs); !errors.Is(err, fleet.ErrConflict) {
		t.Fatalf("empty provenance accepted: %v", err)
	}
	after, err := s.GetQueueProjection(ctx, c.Claim.QueueItem.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected completion changed projection", err)
	}
	original, err := s.GetClaim(ctx, c.Claim.ClaimID)
	if err != nil || original != claim {
		t.Fatal("rejected completion changed original claim", err)
	}
	if _, err = s.GetDispositionByGraphRun(ctx, c.Disposition.GraphRunID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("rejected completion indexed disposition: %v", err)
	}
	if _, err = s.GetRuntimeArtifact(ctx, c.Artifact.ID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("rejected completion published artifact: %v", err)
	}
	if _, err = s.GetVerificationReceipt(ctx, c.Receipts[0].ID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("rejected completion published receipt: %v", err)
	}
	auditAfter, err := s.AuditEvents(ctx)
	if err != nil || !reflect.DeepEqual(auditBefore, auditAfter) {
		t.Fatal("rejected completion changed audit", err)
	}
}
