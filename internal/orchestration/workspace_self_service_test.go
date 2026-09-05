package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	queue "github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

func workspaceFor(t *testing.T, principal string, agent registry.AgentRevision) WorkspaceAuthority {
	t.Helper()
	value, err := NewRegisteredAgentWorkspaceAuthority(principal, revisionRef(agent.AgentID, agent.Revision, agent.Digest), agent.Ownership.OwnerID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestRegisteredAgentWorkspacePublishesLoopWithoutRuntimeOrProvisioningAuthority(t *testing.T) {
	service, repository, _, subject, _, _ := fleetServiceFixture(t)
	workspace := workspaceFor(t, subject.PrincipalID, repository.agent)
	admissions := 0
	service.authorityCommands = fleetAuthorityCommands{admissions: &admissions, err: errors.New("runtime authority must not be consulted")}
	validation := loop.ValidateRevision(repository.loop)
	_, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: workspace.Ref(), Publisher: workspace.Agent, Workspace: &workspace, Publication: loop.PublishRequest{Revision: repository.loop, Validation: validation}})
	if err != nil {
		t.Fatalf("workspace Loop publication: %v", err)
	}
	if admissions != 0 {
		t.Fatalf("workspace publication acquired runtime authority %d time(s)", admissions)
	}
	got := repository.loopPublication.Provenance
	if got.AuthorityKind != "registered-agent-workspace" || got.OwnerID != repository.agent.Ownership.OwnerID || got.PrincipalID != subject.PrincipalID || got.MandateID != workspace.ID || got.StanzaID != "workspace-control" {
		t.Fatalf("workspace provenance was not exact: %+v", got)
	}
	for _, forbidden := range []WorkspaceCapability{"fleet.runtime.execute", "fleet.agents.provision", "credentials.read", "credentials.manage"} {
		if workspace.Allows(forbidden) {
			t.Fatalf("workspace unexpectedly grants %q", forbidden)
		}
	}
}

func TestRegisteredAgentWorkspaceDeniesDisabledStaleAndSubstitutedAgents(t *testing.T) {
	service, repository, _, subject, _, _ := fleetServiceFixture(t)
	workspace := workspaceFor(t, subject.PrincipalID, repository.agent)
	publication := loop.PublishRequest{Revision: repository.loop, Validation: loop.ValidateRevision(repository.loop)}
	tests := []struct {
		name   string
		mutate func(*registry.AgentRevision, *WorkspaceAuthority)
	}{
		{"disabled", func(agent *registry.AgentRevision, _ *WorkspaceAuthority) {
			agent.Lifecycle = registry.LifecycleDisabled
		}},
		{"stale", func(agent *registry.AgentRevision, _ *WorkspaceAuthority) {
			agent.Revision++
			agent.Digest = "sha256:" + strings.Repeat("c", 64)
		}},
		{"substituted owner", func(_ *registry.AgentRevision, value *WorkspaceAuthority) {
			value.OwnerID = "other-owner"
			value.Digest = WorkspaceAuthorityDigest(*value)
		}},
		{"substituted agent", func(_ *registry.AgentRevision, value *WorkspaceAuthority) {
			value.Agent.ID = "agent-2"
			value.ID = "workspace-agent-2"
			value.Digest = WorkspaceAuthorityDigest(*value)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			original := repository.agent
			candidate := workspace
			test.mutate(&repository.agent, &candidate)
			repository.loopPublished = false
			_, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: candidate.Ref(), Publisher: candidate.Agent, Workspace: &candidate, Publication: publication})
			if !errors.Is(err, ErrDenied) || repository.loopPublished {
				t.Fatalf("denial=%v persistence=%t", err, repository.loopPublished)
			}
			repository.agent = original
		})
	}
}

func TestWorkspaceLoopRevisionAndLifecycleDenyCrossAgentTakeover(t *testing.T) {
	service, repository, _, subject, _, _ := fleetServiceFixture(t)
	owner := workspaceFor(t, subject.PrincipalID, repository.agent)
	validation := loop.ValidateRevision(repository.loop)
	if _, err := service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: owner.Ref(), Publisher: owner.Agent, Workspace: &owner, Publication: loop.PublishRequest{Revision: repository.loop, Validation: validation}}); err != nil {
		t.Fatal(err)
	}
	other := repository.agent
	other.AgentID, other.Digest = "agent-2", "sha256:"+strings.Repeat("d", 64)
	other.Ownership = registry.Ownership{OwnerID: "owner-2", AccountabilityID: "accountability-2"}
	repository.agents = map[string]registry.AgentRevision{repository.agent.AgentID: repository.agent, other.AgentID: other}
	takeover := workspaceFor(t, subject.PrincipalID, other)
	v2 := repository.loop
	v2.Revision, v2.PreviousDigest, v2.Digest = 2, repository.loop.Digest, ""
	v2, validation, err := loop.NewRevision(v2)
	if err != nil {
		t.Fatalf("Loop v2: %v (%+v)", err, validation.Issues)
	}
	publication := loop.PublishRequest{Revision: v2, Validation: validation, ExpectedPreviousDigest: repository.loop.Digest}
	if _, err = service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: takeover.Ref(), Publisher: takeover.Agent, Workspace: &takeover, Publication: publication}); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-Agent revision takeover: %v", err)
	}
	if _, err = service.PublishLoop(context.Background(), PublishLoopRequest{Subject: subject, Authority: owner.Ref(), Publisher: owner.Agent, Workspace: &owner, Publication: publication}); err != nil {
		t.Fatalf("owner revision: %v", err)
	}
	repository.loop = v2
	loopRef := revisionRef(v2.LoopID, v2.Revision, v2.Digest)
	if _, _, err = service.SetLoopLifecycle(context.Background(), SetLoopLifecycleRequest{Subject: subject, Authority: takeover.Ref(), Publisher: takeover.Agent, Workspace: &takeover, Loop: loopRef, State: loop.LifecycleActive, EventID: "takeover"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("cross-Agent lifecycle takeover: %v", err)
	}
	if _, _, err = service.SetLoopLifecycle(context.Background(), SetLoopLifecycleRequest{Subject: subject, Authority: owner.Ref(), Publisher: owner.Agent, Workspace: &owner, Loop: loopRef, State: loop.LifecycleActive, EventID: "owner-activate"}); err != nil {
		t.Fatalf("owner lifecycle: %v", err)
	}
}

func TestWorkspaceGraphSupportsSecondRegisteredAgentOnSharedActiveLoopAndImmutableOwner(t *testing.T) {
	service, repository, _, subject, runtimeAuthorityRef, _ := fleetServiceFixture(t)
	owner := workspaceFor(t, subject.PrincipalID, repository.agent)
	other := repository.agent
	other.AgentID, other.Digest = "agent-2", "sha256:"+strings.Repeat("d", 64)
	other.Ownership = registry.Ownership{OwnerID: "owner-2", AccountabilityID: "accountability-2"}
	repository.agents = map[string]registry.AgentRevision{repository.agent.AgentID: repository.agent, other.AgentID: other}
	loopRef := revisionRef(repository.loop.LoopID, repository.loop.Revision, repository.loop.Digest)
	otherRef := revisionRef(other.AgentID, other.Revision, other.Digest)
	authorityRef := owner.Ref()
	revision, validation, err := graph.NewRevision(graph.GraphRevision{GraphID: "workspace-graph", Revision: 1, OwnerAgent: &owner.Agent, PublicationAuthority: &authorityRef, PublishedByPrincipal: subject.PrincipalID, Nodes: []graph.Node{{ID: "owner-node", Participant: owner.Agent, Loop: loopRef}, {ID: "shared-node", Participant: otherRef, Loop: loopRef}}})
	if err != nil {
		t.Fatalf("Graph: %v (%+v)", err, validation.Issues)
	}
	if _, err = service.PublishGraph(context.Background(), PublishGraphRequest{Subject: subject, Authority: owner.Ref(), Workspace: &owner, Publication: graph.PublishRequest{Revision: revision, Validation: validation}}); err != nil {
		t.Fatalf("publish shared Graph: %v", err)
	}
	if repository.graphPublication.Revision.OwnerAgent == nil || *repository.graphPublication.Revision.OwnerAgent != owner.Agent {
		t.Fatalf("Graph owner not preserved: %+v", repository.graphPublication.Revision.OwnerAgent)
	}
	takeover := workspaceFor(t, subject.PrincipalID, other)
	repository.graph = revision
	repository.graphLifecycle = graph.Lifecycle{GraphID: revision.GraphID, State: graph.LifecycleActive, ActiveRevision: revision.Revision, ActiveDigest: revision.Digest}
	participantRequest := workspaceSubmit(subject, takeover, revisionRef(revision.GraphID, revision.Revision, revision.Digest), "participant")
	decision, err := service.PrepareGraphRun(context.Background(), participantRequest)
	if err != nil || decision.Accepted == nil || decision.Accepted.Submission.OwnerAgentID != other.AgentID || len(decision.Accepted.Snapshot.Participants) != 2 || decision.Accepted.Snapshot.Participants[0] != owner.Agent || decision.Accepted.Snapshot.Participants[1] != otherRef {
		t.Fatalf("second participant workspace queue was not exact: decision=%+v err=%v", decision, err)
	}
	v2 := revision
	v2.Revision, v2.PreviousDigest, v2.Digest = 2, revision.Digest, ""
	v2.OwnerAgent = &takeover.Agent
	takeoverAuthority := takeover.Ref()
	v2.PublicationAuthority = &takeoverAuthority
	v2.PublishedByPrincipal = subject.PrincipalID
	v2, validation, err = graph.NewRevision(v2)
	if err != nil {
		t.Fatal(err)
	}
	repository.graph = revision
	if _, err = service.PublishGraph(context.Background(), PublishGraphRequest{Subject: subject, Authority: takeover.Ref(), Workspace: &takeover, Publication: graph.PublishRequest{Revision: v2, Validation: validation}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("Graph owner takeover: %v", err)
	}
	if _, err = service.PublishGraph(context.Background(), PublishGraphRequest{Subject: subject, Authority: runtimeAuthorityRef, Publication: graph.PublishRequest{Revision: v2, Validation: validation}}); !errors.Is(err, ErrDenied) {
		t.Fatalf("legacy authority crossed workspace Graph ownership: %v", err)
	}
}

func TestWorkspaceSubmissionRequiresActiveGraphAndPinsExactParticipantQueue(t *testing.T) {
	service, repository, _, subject, _, graphRef := fleetServiceFixture(t)
	workspace := workspaceFor(t, subject.PrincipalID, repository.agent)
	request := workspaceSubmit(subject, workspace, graphRef, "submission")
	repository.graphLifecycle = graph.Lifecycle{GraphID: repository.graph.GraphID, State: graph.LifecycleDraft}
	decision, err := service.PrepareGraphRun(context.Background(), request)
	if err != nil || decision.Rejection == nil || decision.Rejection.ReasonCode != "graph_inactive" || repository.accepted != nil {
		t.Fatalf("inactive Graph decision=%+v err=%v", decision, err)
	}
	repository.graphLifecycle = graph.Lifecycle{GraphID: repository.graph.GraphID, State: graph.LifecycleActive, ActiveRevision: repository.graph.Revision, ActiveDigest: repository.graph.Digest}
	decision, err = service.PrepareGraphRun(context.Background(), request)
	if err != nil || decision.Accepted == nil {
		t.Fatalf("active Graph decision=%+v err=%v", decision, err)
	}
	accepted := decision.Accepted
	if accepted.InitialTransition.To != queue.StateAwaitingRuntime || accepted.Submission.AuthorityKind != "registered-agent-workspace" || accepted.Submission.OwnerAgentID != workspace.Agent.ID || len(accepted.Snapshot.Participants) != 1 || accepted.Snapshot.Participants[0] != workspace.Agent {
		t.Fatalf("workspace queue did not pin exact participant/owner: %+v", accepted)
	}
}

func workspaceSubmit(subject core.Subject, workspace WorkspaceAuthority, graphRef reference.RevisionRef, suffix string) SubmitGraphRequest {
	return SubmitGraphRequest{Subject: subject, Authority: workspace.Ref(), Workspace: &workspace, Graph: graphRef, SubmissionID: suffix + "-submission", IdempotencyKey: suffix + "-submit", SnapshotID: suffix + "-snapshot", QueueItemID: suffix + "-queue", GraphRunID: suffix + "-run", TransitionID: suffix + "-await", RejectionID: suffix + "-reject", MaxAttempts: 1}
}

func TestWorkspaceRuntimeHandoffProcessesOnlyWithSameAgentAuthority(t *testing.T) {
	service, repository, _, subject, runtimeAuthority, graphRef := fleetServiceFixture(t)
	workspace := workspaceFor(t, subject.PrincipalID, repository.agent)
	request := workspaceSubmit(subject, workspace, graphRef, "handoff")
	if decision, err := service.PrepareGraphRun(context.Background(), request); err != nil || decision.Accepted == nil {
		t.Fatalf("prepare: %+v %v", decision, err)
	}
	worker := newLifecycleTestWorker(t, repository, service)
	other := repository.agent
	other.AgentID, other.Digest, other.Ownership.OwnerID = "agent-2", "sha256:"+strings.Repeat("e", 64), "owner-2"
	repository.agents = map[string]registry.AgentRevision{repository.agent.AgentID: repository.agent, other.AgentID: other}
	otherWorkspace := workspaceFor(t, subject.PrincipalID, other)
	if _, _, err := worker.BindRuntime(context.Background(), BindQueueRuntimeRequest{Subject: subject, Workspace: &otherWorkspace, Authority: runtimeAuthority, QueueItemID: request.QueueItemID, BindingID: "cross-bind", TransitionID: "cross-transition"}); !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("cross-Agent bind: %v", err)
	}
	binding, created, err := worker.BindRuntime(context.Background(), BindQueueRuntimeRequest{Subject: subject, Workspace: &workspace, Authority: runtimeAuthority, QueueItemID: request.QueueItemID, BindingID: "owner-bind", TransitionID: "owner-transition"})
	if err != nil || !created || binding.OwnerAgent != workspace.Agent || binding.Authority != runtimeAuthority {
		t.Fatalf("same-Agent binding=%+v created=%t err=%v", binding, created, err)
	}
	blobs, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(blobs)
	if err != nil {
		t.Fatal(err)
	}
	worker, err = NewQueueWorker(repository, service, blobs, verifier, fixedRuntimeAdapter{}, service.now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := worker.Process(context.Background(), WorkRequest{Subject: subject, Authority: runtimeAuthority, QueueItemID: request.QueueItemID, WorkerID: "worker", LoopExecutionID: "handoff-loop", ClaimID: "handoff-claim", AttemptID: "handoff-attempt", ClaimTransitionID: "handoff-claimed", TerminalTransitionID: "handoff-done", DispositionID: "handoff-disposition", ArtifactID: "handoff-artifact", LeaseDuration: time.Minute})
	if err != nil || result.Disposition.State != execution.StateSucceeded {
		t.Fatalf("bound process result=%+v err=%v", result, err)
	}
}

func TestWorkspaceOwnerQueueTerminalLifecycleAndCrossAgentDenial(t *testing.T) {
	tests := []struct {
		name     string
		state    queue.State
		attempts uint32
		invoke   func(*QueueWorker, QueueTerminalRequest) (queue.Cancellation, error)
	}{
		{"cancel", queue.StateAwaitingRuntime, 0, func(w *QueueWorker, r QueueTerminalRequest) (queue.Cancellation, error) {
			return w.Cancel(context.Background(), r)
		}},
		{"revoke", queue.StateAwaitingRuntime, 0, func(w *QueueWorker, r QueueTerminalRequest) (queue.Cancellation, error) {
			return w.Revoke(context.Background(), r)
		}},
		{"expire", queue.StateClaimed, 1, func(w *QueueWorker, r QueueTerminalRequest) (queue.Cancellation, error) {
			return w.Expire(context.Background(), r)
		}},
		{"exhaust", queue.StateClaimed, 1, func(w *QueueWorker, r QueueTerminalRequest) (queue.Cancellation, error) {
			return w.Exhaust(context.Background(), r)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, repository, _, subject, _, graphRef := fleetServiceFixture(t)
			workspace := workspaceFor(t, subject.PrincipalID, repository.agent)
			request := workspaceSubmit(subject, workspace, graphRef, "terminal")
			if decision, err := service.PrepareGraphRun(context.Background(), request); err != nil || decision.Accepted == nil {
				t.Fatalf("prepare: %v", err)
			}
			item := repository.accepted.QueueItem
			repository.projection = queue.Projection{QueueItemID: item.ItemID, State: test.state, Attempts: test.attempts}
			if test.state == queue.StateClaimed {
				repository.claim, _ = queue.NewClaim(queue.Claim{ClaimID: "terminal-claim", QueueItem: digestRef(item.ItemID, item.Digest), AttemptID: "terminal-attempt", WorkerID: "worker", Authority: digestRef("runtime", "sha256:"+strings.Repeat("f", 64)), ClaimedAt: service.now().Add(-2 * time.Minute), ExpiresAt: service.now().Add(-time.Minute)})
				repository.projection.ActiveClaimID = repository.claim.ClaimID
				repository.attempt, _ = execution.NewAttempt(execution.Attempt{AttemptID: repository.claim.AttemptID, GraphRunID: item.GraphRunID, LoopExecutionID: "terminal-loop", QueueItem: repository.claim.QueueItem, ClaimID: repository.claim.ClaimID, AttemptNumber: 1, CreatedAt: service.now().Add(-2 * time.Minute)})
			}
			worker := newLifecycleTestWorker(t, repository, service)
			other := repository.agent
			other.AgentID, other.Digest, other.Ownership.OwnerID = "agent-2", "sha256:"+strings.Repeat("e", 64), "owner-2"
			repository.agents = map[string]registry.AgentRevision{repository.agent.AgentID: repository.agent, other.AgentID: other}
			otherWorkspace := workspaceFor(t, subject.PrincipalID, other)
			base := QueueTerminalRequest{Subject: subject, Authority: workspace.Ref(), QueueItemID: item.ItemID, CancellationID: "terminal-action", TransitionID: "terminal-transition"}
			cross := base
			cross.Workspace, cross.Authority = &otherWorkspace, otherWorkspace.Ref()
			if _, err := test.invoke(worker, cross); !errors.Is(err, ErrWorkerDenied) {
				t.Fatalf("cross-Agent operation: %v", err)
			}
			base.Workspace = &workspace
			if _, err := test.invoke(worker, base); err != nil {
				t.Fatalf("owner operation: %v", err)
			}
		})
	}
}

func TestWorkspaceOwnerQueueRetryAndCrossAgentDenial(t *testing.T) {
	service, repository, _, subject, _, graphRef := fleetServiceFixture(t)
	workspace := workspaceFor(t, subject.PrincipalID, repository.agent)
	request := workspaceSubmit(subject, workspace, graphRef, "retry")
	request.MaxAttempts = 2
	if decision, err := service.PrepareGraphRun(context.Background(), request); err != nil || decision.Accepted == nil {
		t.Fatalf("prepare: %v", err)
	}
	item := repository.accepted.QueueItem
	repository.claim, _ = queue.NewClaim(queue.Claim{ClaimID: "retry-claim", QueueItem: digestRef(item.ItemID, item.Digest), AttemptID: "retry-attempt", WorkerID: "worker", Authority: digestRef("runtime", "sha256:"+strings.Repeat("f", 64)), ClaimedAt: service.now().Add(-2 * time.Minute), ExpiresAt: service.now().Add(-time.Minute)})
	repository.projection = queue.Projection{QueueItemID: item.ItemID, State: queue.StateClaimed, Attempts: 1, ActiveClaimID: repository.claim.ClaimID}
	worker := newLifecycleTestWorker(t, repository, service)
	other := repository.agent
	other.AgentID, other.Digest, other.Ownership.OwnerID = "agent-2", "sha256:"+strings.Repeat("e", 64), "owner-2"
	repository.agents = map[string]registry.AgentRevision{repository.agent.AgentID: repository.agent, other.AgentID: other}
	otherWorkspace := workspaceFor(t, subject.PrincipalID, other)
	cross := QueueRetryRequest{Subject: subject, Authority: otherWorkspace.Ref(), Workspace: &otherWorkspace, QueueItemID: item.ItemID, RetryID: "cross-retry", TransitionID: "cross-retried", Reclaimed: true}
	if _, err := worker.Retry(context.Background(), cross); !errors.Is(err, ErrWorkerDenied) {
		t.Fatalf("cross-Agent retry: %v", err)
	}
	owner := QueueRetryRequest{Subject: subject, Authority: workspace.Ref(), Workspace: &workspace, QueueItemID: item.ItemID, RetryID: "owner-retry", TransitionID: "owner-retried", Reclaimed: true}
	if _, err := worker.Retry(context.Background(), owner); err != nil {
		t.Fatalf("owner retry: %v", err)
	}
}

func TestWorkspaceAuthorityCannotRepresentCredentialManagement(t *testing.T) {
	_, repository, _, subject, _, _ := fleetServiceFixture(t)
	workspace := workspaceFor(t, subject.PrincipalID, repository.agent)
	if len(workspace.Capabilities) != 6 {
		t.Fatalf("unexpected workspace capabilities: %v", workspace.Capabilities)
	}
	for _, capability := range workspace.Capabilities {
		name := string(capability)
		if strings.Contains(name, "credential") || strings.Contains(name, "secret") || strings.Contains(name, "runtime") || strings.Contains(name, "provision") {
			t.Fatalf("delegated workspace crossed protected boundary with %q", capability)
		}
	}
}
