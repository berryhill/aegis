package badger

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"path/filepath"
	"testing"
	"time"
)

func TestCompletionExpiryExactClaimOnly(t *testing.T) {
	for _, mode := range []string{"boundary", "after", "late-success", "late-failure", "stale", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, c, blobs := completionFixture(t, ctx, filepath.Join(t.TempDir(), schemaVersion))
			defer s.Close()
			c.Disposition.OccurredAt = c.Claim.ExpiresAt
			if mode == "after" {
				c.Disposition.OccurredAt = c.Claim.ExpiresAt.Add(time.Second)
			}
			c.Transition.OccurredAt = c.Disposition.OccurredAt
			if mode != "late-success" {
				c.Disposition.State = execution.StateExpired
				c.Disposition.ReasonCode = "execution_expired"
				c.Transition.To = queue.StateExpired
				c.Transition.Reason = c.Disposition.ReasonCode
			}
			if mode == "late-failure" {
				c.Disposition.State = execution.StateFailed
				c.Transition.To = queue.StateFailed
			}
			var err error
			c.Disposition, err = disposition.New(c.Disposition)
			if err != nil {
				t.Fatal(err)
			}
			c.Transition, err = queue.NewTransition(c.Transition)
			if err != nil {
				t.Fatal(err)
			}
			fact := completionAudit(c)
			if mode != "late-success" {
				c.Artifact = nil
				c.Receipts = nil
				c.Disposition.ArtifactIDs = nil
				c.Disposition.ReceiptIDs = nil
				c.Disposition, err = disposition.New(c.Disposition)
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "stale" || mode == "replaced" {
				at := c.Claim.ExpiresAt
				retry := mustQueueRetry(t, queue.Retry{RetryID: "expiry-reclaim", QueueItem: c.Claim.QueueItem, ClaimID: c.Claim.ClaimID, AttemptNumber: 1, AvailableAt: at, Reclaimed: true, Reason: "lease expired", OccurredAt: at})
				tr := mustQueueTransition(t, queue.QueueTransition{TransitionID: "expiry-requeued", QueueItemID: c.Claim.QueueItem.ID, From: queue.StateClaimed, To: queue.StateQueued, ClaimID: c.Claim.ClaimID, Reason: retry.Reason, OccurredAt: at})
				if err = s.RetryQueueItem(ctx, fleet.RetryMutation{Retry: retry, Transition: tr}, audit("queue.retried", retry.RetryID)); err != nil {
					t.Fatal(err)
				}
				if mode == "replaced" {
					claim := c.Claim
					claim.ClaimID = "replacement"
					claim.AttemptID = "replacement-attempt"
					claim.ClaimedAt = at
					claim.ExpiresAt = at.Add(time.Minute)
					claim, err = queue.NewClaim(claim)
					if err != nil {
						t.Fatal(err)
					}
					attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: claim.AttemptID, GraphRunID: c.Disposition.GraphRunID, LoopExecutionID: c.Disposition.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: 2, CreatedAt: at})
					if err != nil {
						t.Fatal(err)
					}
					tr = mustQueueTransition(t, queue.QueueTransition{TransitionID: "replacement-claimed", QueueItemID: claim.QueueItem.ID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "worker lease acquired", OccurredAt: at})
					if err = s.ClaimQueueItem(ctx, claim, attempt, tr, audit("queue.claimed", claim.ClaimID)); err != nil {
						t.Fatal(err)
					}
				}
			}
			err = s.CompleteQueueItem(ctx, c, fact, blobs)
			if mode != "boundary" && mode != "after" {
				if !errors.Is(err, fleet.ErrConflict) {
					t.Fatalf("unsafe completion: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.GetQueueProjection(ctx, c.Claim.QueueItem.ID)
			if err != nil || p.State != queue.StateExpired || p.ActiveClaimID != "" {
				t.Fatalf("projection: %+v %v", p, err)
			}
			d, err := s.GetDispositionByGraphRun(ctx, c.Disposition.GraphRunID)
			if err != nil || d.State != execution.StateExpired || !d.OccurredAt.Equal(c.Disposition.OccurredAt) {
				t.Fatalf("disposition: %+v %v", d, err)
			}
		})
	}
}
