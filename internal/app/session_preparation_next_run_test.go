package app

import (
	"context"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/disposition"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"testing"
)

type predecessorRepository struct {
	fleet.Repository
	revision   loop.LoopRevision
	binding    queue.RuntimeBinding
	projection queue.Projection
	unbound    bool
}

func (r predecessorRepository) GetLoopRevision(context.Context, string, uint64) (loop.LoopRevision, error) {
	return r.revision, nil
}
func (r predecessorRepository) ListQueueItems(context.Context) ([]queue.Item, error) {
	return []queue.Item{{ItemID: "shared", Submission: reference.DigestRef{ID: "submission", Digest: "digest"}}}, nil
}
func (r predecessorRepository) GetQueueRuntimeBinding(context.Context, string) (queue.RuntimeBinding, error) {
	if r.unbound {
		return queue.RuntimeBinding{}, fleet.ErrNotFound
	}
	return r.binding, nil
}
func (r predecessorRepository) GetSubmission(context.Context, string) (queue.Submission, error) {
	return queue.Submission{Digest: "digest", Authority: r.binding.Authority}, nil
}
func (r predecessorRepository) GetQueueProjection(context.Context, string) (queue.Projection, error) {
	return r.projection, nil
}

// Synthetic source-level eligibility tests, not live-runtime evidence.
func TestCompletedReusablePredecessorEligibility(t *testing.T) {
	for _, name := range []string{"success", "different-next-loop", "terminal-failure", "busy", "active-claim", "nonterminal", "foreign-owner", "foreign-subject", "foreign-loop", "fence-drift", "prepared-drift", "binding-drift", "charter-drift", "missing-attempt", "terminal-evidence-drift", "failed-budget", "failed-budget-after-manual-retirement", "cancelled", "pending"} {
		t.Run(name, func(t *testing.T) {
			s := testService(t)
			ctx := context.Background()
			sub := core.Subject{ID: "subject", PrincipalID: "owner"}
			agent := registry.AgentRevision{AgentID: "agent", Digest: "agent-digest"}
			agent.Charter.Digest = "charter"
			agent.Charter.Revision = 1
			in := QueueLoopInput{Agent: reference.RevisionRef{ID: "agent", Revision: 1, Digest: agent.Digest}, Loop: reference.RevisionRef{ID: "loop", Revision: 1, Digest: "loop-digest"}}
			s.FleetRepository = predecessorRepository{revision: loop.LoopRevision{SchemaVersion: loop.DoerReusableSchemaVersion, Digest: in.Loop.Digest}}
			ref := reference.DigestRef{ID: "queue", Digest: "queue-digest"}
			authref := reference.DigestRef{ID: "authority", Digest: "authority-digest"}
			authority := core.AuthorityContext{ID: authref.ID, SubjectID: sub.ID, AgentID: agent.AgentID, CharterDigest: agent.Charter.Digest, SessionID: "session", MandateID: "mandate"}
			session := core.Session{ID: "session", Status: "running"}
			session.Mandate.Subject = sub
			session.Mandate.AgentID = agent.AgentID
			session.Mandate.CharterDigest = agent.Charter.Digest
			session.Mandate.CharterRevision = 1
			b := queue.RuntimeBinding{QueueItem: ref, OwnerAgent: in.Agent, Authority: authref, MandateID: "mandate"}
			old := QueueExecutionView{SourceKind: graph.SourceDirectLoop, Source: in.Loop, Item: queue.Item{ItemID: ref.ID, Digest: ref.Digest, GraphRunID: "run"}, Submission: queue.Submission{OwnerID: sub.PrincipalID, OwnerAgentID: agent.AgentID}, RuntimeAuthorityBinding: &b, Projection: queue.Projection{State: queue.StateSucceeded, LastTransitionID: "terminal"}, LoopExecutions: []execution.LoopExecution{{LoopExecutionID: "child", GraphRunID: "run", Loop: in.Loop, Participant: in.Agent}}, Attempts: []execution.Attempt{{AttemptID: "attempt", LoopExecutionID: "child", GraphRunID: "run", QueueItem: ref, ClaimID: "claim"}}, Claims: []queue.Claim{{ClaimID: "claim", AttemptID: "attempt", QueueItem: ref, Authority: authref}}, Disposition: &disposition.Record{State: execution.StateSucceeded, GraphRunID: "run", LoopExecutionID: "child", AttemptID: "attempt", QueueItem: ref, Authority: authref}, Transitions: []queue.QueueTransition{{TransitionID: "terminal", From: queue.StateClaimed, To: queue.StateSucceeded, ClaimID: "claim"}}}
			fence := map[string]string{"queue_item_id": ref.ID, "agent_digest": agent.Digest}
			prepared := authref
			switch name {
			case "different-next-loop":
				in.Loop = reference.RevisionRef{ID: "different-loop", Revision: 1, Digest: "different-loop-digest"}
			case "terminal-failure":
				old.Projection.State = queue.StateFailed
				old.Disposition.State = execution.StateFailed
				old.Transitions[0].To = queue.StateFailed
			case "busy":
				s.loopRunWorkers = map[string]bool{ref.ID: true}
			case "active-claim":
				old.Projection.ActiveClaimID = "claim"
			case "nonterminal", "pending":
				old.Projection.State = queue.StatePreparationPending
			case "foreign-owner":
				old.Submission.OwnerID = "foreign"
			case "foreign-subject":
				session.Mandate.Subject.ID = "foreign"
			case "foreign-loop":
				old.Source.Digest = "foreign"
			case "fence-drift":
				fence["agent_digest"] = "foreign"
			case "prepared-drift":
				prepared.Digest = "foreign"
			case "binding-drift":
				b.MandateID = "foreign"
			case "charter-drift":
				session.Mandate.CharterDigest = "foreign"
			case "missing-attempt":
				old.Attempts = nil
			case "terminal-evidence-drift":
				old.Transitions = nil
			case "failed-budget", "failed-budget-after-manual-retirement":
				old.Projection.State = queue.StateFailed
				old.Disposition.State = execution.StateFailed
				session.Status = "terminated"
			case "cancelled":
				old.Projection.State = queue.StateCancelled
			}
			if err := s.Store.Create("queue-session-prepared", "parent", prepared); err != nil {
				t.Fatal(err)
			}
			got := s.completedReusablePredecessor(ctx, sub, in, agent, "parent", fence, old, authority, session)
			if got != (name == "success" || name == "different-next-loop" || name == "terminal-failure") {
				t.Fatalf("eligibility=%v", got)
			}
		})
	}
}

func TestPredecessorAuthorityIdleRejectsSharedWork(t *testing.T) {
	auth := reference.DigestRef{ID: "authority", Digest: "digest"}
	for _, unbound := range []bool{false, true} {
		for _, state := range []queue.State{queue.StateQueued, queue.StatePreparationPending, queue.StateAwaitingRuntime, queue.StateClaimed, queue.State("unknown"), queue.StateSucceeded} {
			s := &Service{FleetRepository: predecessorRepository{binding: queue.RuntimeBinding{Authority: auth}, projection: queue.Projection{State: state}, unbound: unbound}}
			err := s.predecessorAuthorityIdle(context.Background(), auth)
			if (err == nil) != (state == queue.StateSucceeded) {
				t.Fatalf("unbound=%v state=%s err=%v", unbound, state, err)
			}
		}
	}
	s := &Service{FleetRepository: predecessorRepository{binding: queue.RuntimeBinding{Authority: auth}, projection: queue.Projection{State: queue.StateSucceeded, ActiveClaimID: "claim"}}}
	if s.predecessorAuthorityIdle(context.Background(), auth) == nil {
		t.Fatal("terminal projection with active claim accepted")
	}
}
