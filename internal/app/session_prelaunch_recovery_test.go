package app

import (
	"context"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestCompletedPrelaunchRefusalRecoveryKeepsFenceAndQueue(t *testing.T) {
	for _, name := range []string{"valid", "unknown-outcome", "wrong-intent", "wrong-subject", "wrong-loop", "already-prepared", "future-refusal"} {
		t.Run(name, func(t *testing.T) {
			s := testService(t)
			now := s.Now()
			sub := core.Subject{ID: "subject", PrincipalID: "owner"}
			agent := registry.AgentRevision{AgentID: "agent", Digest: "agent-digest"}
			in := QueueLoopInput{Loop: reference.RevisionRef{Digest: "loop-digest"}}
			result := QueueLoopResult{RequestID: "request", QueueItemID: "queue"}
			fence := map[string]string{"queue_item_id": "queue", "agent_digest": agent.Digest, "request_id": "request", "subject_id": sub.ID, "loop_digest": in.Loop.Digest}
			tr := queue.QueueTransition{TransitionID: "diagnostic", From: queue.StatePreparationPending, To: queue.StatePreparationPending, Reason: "prior_runtime_authority_requires_explicit_recovery", OccurredAt: now, Digest: "transition-digest"}
			switch name {
			case "unknown-outcome":
				tr.Reason = "session_preparation_in_progress_or_interrupted"
			case "wrong-intent":
				fence["request_id"] = "foreign"
			case "wrong-subject":
				fence["subject_id"] = "foreign"
			case "wrong-loop":
				fence["loop_digest"] = "foreign"
			case "future-refusal":
				tr.OccurredAt = now.Add(time.Second)
			case "already-prepared":
				if err := s.Store.Create("queue-session-prepared", "parent", reference.DigestRef{ID: "authority"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Store.Create("queue-session-preparation", "parent", fence); err != nil {
				t.Fatal(err)
			}
			view := QueueExecutionView{Item: queue.Item{ItemID: "queue", EnqueuedAt: now.Add(-time.Second)}, Projection: queue.Projection{State: queue.StatePreparationPending, LastTransitionID: tr.TransitionID}}
			next, fact, err := s.recoverCompletedPrelaunchBlock(context.Background(), sub, in, result, agent, "parent", fence, view, tr)
			if name != "valid" {
				if err == nil {
					t.Fatal("unsafe prelaunch recovery accepted")
				}
				return
			}
			if err != nil || fact == nil || !fact.RetrySafe || next != preparationNextID("parent") || fact.MandateID != "" || fact.AuthorityID != "" {
				t.Fatal("safe refusal not recovered", err)
			}
			var retained map[string]string
			if err = s.Store.Load("queue-session-preparation", "parent", &retained); err != nil || retained["queue_item_id"] != "queue" {
				t.Fatal("old fence lost", err)
			}
			if err = s.Store.Load("queue-session-preparation", next, &retained); err != nil || retained["queue_item_id"] != "queue" || retained["parent_reservation"] != "parent" {
				t.Fatal("identity changed", err)
			}
		})
	}
}
