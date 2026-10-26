//go:build linux

package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"os/exec"
	"testing"
	"time"
)

func exerciseContributionContinuation(t *testing.T, s *Service, repo *fleetbadger.Store, sub core.Subject, c contribution.Contract, state contribution.Continuity) contribution.Continuity {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	input := func() ContributionContinuityInput {
		return ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest}
	}
	old := state.RoleAuthorities["builder"]
	item, err := repo.GetQueueItem(ctx, c.QueueItemID)
	must(err)
	le, err := execution.NewLoopExecution(execution.LoopExecution{LoopExecutionID: "continuation-execution", GraphRunID: c.RunID, GraphNodeID: "implement", Loop: c.Loop, Participant: c.Controller, CreatedAt: s.Now()})
	must(err)
	fact := fleet.AuditFact{Event: core.AuditEvent{Type: "continuation.fixture", SubjectID: sub.ID, PrincipalID: sub.PrincipalID, Outcome: "ok", Reason: "isolated"}}
	_, err = repo.CreateLoopExecution(ctx, le, fact)
	must(err)
	root, err := repo.GetQueueRuntimeBinding(ctx, c.QueueItemID)
	must(err)
	claim, err := queue.NewClaim(queue.Claim{ClaimID: "continuation-claim", QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: item.ItemID, Digest: item.Digest}, AttemptID: "continuation-attempt", WorkerID: "continuation-worker", Authority: root.Authority, ClaimedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)})
	must(err)
	attempt, err := execution.NewAttempt(execution.Attempt{AttemptID: claim.AttemptID, GraphRunID: c.RunID, LoopExecutionID: le.LoopExecutionID, QueueItem: claim.QueueItem, ClaimID: claim.ClaimID, AttemptNumber: 1, CreatedAt: s.Now()})
	must(err)
	transition, err := queue.NewTransition(queue.QueueTransition{TransitionID: "continuation-claimed", QueueItemID: c.QueueItemID, From: queue.StateQueued, To: queue.StateClaimed, ClaimID: claim.ClaimID, Reason: "isolated_native_producer", OccurredAt: s.Now()})
	must(err)
	must(repo.ClaimQueueItem(ctx, claim, attempt, transition, fact))
	// This fixture has no ManagedContribution producer; the isolated native
	// producer exclusively owns all native commands. Legacy worker stays excluded.
	worker := s.QueueWorker
	s.QueueWorker = nil
	defer func() { s.QueueWorker = worker }()
	cmd := exec.Command("/usr/bin/true")
	b := contribution.NativeBinding{IntentID: "continuation-native", RunID: c.RunID, QueueItemID: c.QueueItemID, ClaimID: claim.ClaimID, AttemptID: claim.AttemptID, Role: "builder", Epoch: old.Epoch, Authority: old.Ref(), Candidate: state.Candidate, PolicyDigest: c.TemplateDigest, CommandDigest: hermes.NativeCommandDigest(cmd)}
	must(s.runIsolatedContributionNativeBatchAs(ctx, sub, old.SessionID, c.Key, []contribution.NativeBinding{b}, []*exec.Cmd{cmd}))
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, sub, c.Key)
	must(err)
	if _, err = s.ResumeContributionRoleAs(ctx, sub, c.Key, "builder", input()); err == nil {
		t.Fatal("missing stop admitted")
	}
	state, err = s.StopContributionRoleAs(ctx, sub, c.Key, "builder", input())
	must(err)
	stale := input()
	stale.ExpectedSequence--
	if _, err = s.ResumeContributionRoleAs(ctx, sub, c.Key, "builder", stale); err == nil {
		t.Fatal("stale admitted")
	}
	foreign := sub
	foreign.PrincipalID = "foreign"
	if _, err = s.ResumeContributionRoleAs(ctx, foreign, c.Key, "builder", input()); err == nil {
		t.Fatal("foreign admitted")
	}
	type resumeResult struct {
		session core.Session
		err     error
	}
	outcomes := make(chan resumeResult, 2)
	head := input()
	for i := 0; i < 2; i++ {
		go func() {
			session, e := s.ResumeContributionRoleAs(ctx, sub, c.Key, "builder", head)
			outcomes <- resumeResult{session, e}
		}()
	}
	first, second := <-outcomes, <-outcomes
	if first.err != nil {
		first, second = second, first
	}
	must(first.err)
	if second.err == nil {
		t.Fatal("concurrent duplicate issued")
	}
	session := first.session
	t.Cleanup(func() { _ = s.TerminateSession(ctx, session.ID, "cleanup") })
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, sub, c.Key)
	must(err)
	if state.RoleAuthorities["builder"].Epoch != 2 || state.RoleAuthorities["builder"].SessionID == old.SessionID {
		t.Fatal("no fresh epoch")
	}
	_, err = s.CheckContributionRoleAs(ctx, sub, session.ID, c.Key, "builder", input())
	must(err)
	if _, err = s.CheckContributionRoleAs(ctx, sub, old.SessionID, c.Key, "builder", input()); err == nil {
		t.Fatal("old role admitted")
	}
	if _, err = s.ResumeContributionRoleAs(ctx, sub, c.Key, "builder", input()); err == nil {
		t.Fatal("duplicate issued")
	}
	after, err := repo.GetQueueRuntimeBinding(ctx, c.QueueItemID)
	must(err)
	if after != root || state.Used.QueueAttempts > 1 {
		t.Fatal("binding/budgets changed")
	}
	return state
}
