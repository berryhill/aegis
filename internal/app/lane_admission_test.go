package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func TestPublishAndReadinessEnforceCharterLaneEnvelope(t *testing.T) {
	ctx := context.Background()
	s, repo, subject, _, _ := contributionAuthorityFixture(t, func(c *core.Charter) {
		h := &c.Stanzas[0].Hermes
		h.AllowedLanes = []core.HermesLane{
			{ID: "default", Provider: h.Provider, Model: h.Model, ProviderAuthentication: h.ProviderAuthentication, LocalInference: h.LocalInference},
			{ID: "secondary", Provider: "test", Model: "secondary-model"},
		}
	})

	controllerAgent, err := repo.LatestAgentRevision(ctx, "contribution-controller")
	if err != nil {
		t.Fatal(err)
	}
	builderAgent, err := repo.LatestAgentRevision(ctx, "contribution-builder")
	if err != nil {
		t.Fatal(err)
	}
	testerAgent, err := repo.LatestAgentRevision(ctx, "contribution-tester")
	if err != nil {
		t.Fatal(err)
	}
	refs := []reference.RevisionRef{
		{SchemaVersion: reference.RevisionRefSchemaVersion, ID: controllerAgent.AgentID, Revision: controllerAgent.Revision, Digest: controllerAgent.Digest},
		{SchemaVersion: reference.RevisionRefSchemaVersion, ID: builderAgent.AgentID, Revision: builderAgent.Revision, Digest: builderAgent.Digest},
		{SchemaVersion: reference.RevisionRefSchemaVersion, ID: testerAgent.AgentID, Revision: testerAgent.Revision, Digest: testerAgent.Digest},
	}

	draft := loop.ImplementationDraft("Implement lane test", "TestValue passes")
	draft.Workspace = t.TempDir()
	draft.WritableFiles = []string{"value.go"}
	draft.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}

	profile := loop.ContributionProfile{
		Controller:      refs[0],
		Builder:         refs[1],
		Tester:          refs[2],
		ControllerLane:  &loop.RoleModelLane{Provider: "test", Model: "secondary-model"},
		BuilderLane:     &loop.RoleModelLane{Model: "secondary-model"},
		TesterLane:      &loop.RoleModelLane{Provider: "test", Model: "secondary-model"},
		Repository:      "owner/repo",
		RepositoryID:    7,
		Base:            strings.Repeat("b", 40),
		ReadAction:      contribution.ReadIssueAction,
		Effect:          "local-only",
		DeadlineSeconds: 60,
		Invariants:      []string{"preserve unrelated changes"},
		Exclusions:      []string{"no publication or merge"},
	}
	validContrib, _, err := loop.NewContributionRevision("contrib-lane-ok", 1, "", draft, profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishLoopAs(ctx, subject, PublishLoopInput{AgentID: refs[0].ID, Revision: validContrib, IdempotencyKey: "pub-contrib-ok"}); err != nil {
		t.Fatalf("expected authorized contribution role lanes to publish: %v", err)
	}

	badProfile := profile
	badProfile.BuilderLane = &loop.RoleModelLane{Provider: "test", Model: "unapproved-model"}
	invalidContrib, _, err := loop.NewContributionRevision("contrib-lane-bad", 1, "", draft, badProfile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PublishLoopAs(ctx, subject, PublishLoopInput{AgentID: refs[0].ID, Revision: invalidContrib, IdempotencyKey: "pub-contrib-bad"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("expected unauthorized builder lane to fail PublishLoopAs with ErrDenied, got %v", err)
	}

	// Ordinary Loop & Graph lane admission
	makeActionLoop := func(loopID, provider, model string) loop.LoopRevision {
		rev, _, e := loop.NewRevision(loop.LoopRevision{
			SchemaVersion: loop.RevisionSchemaVersion,
			LoopID:        loopID,
			Revision:      1,
			Name:          loopID,
			EntryStepID:   "act",
			Steps: []loop.Step{
				{
					ID:       "act",
					Kind:     loop.StepAction,
					Provider: provider,
					Model:    model,
					Retry:    loop.RetryPolicy{MaxAttempts: 1},
					EvidenceClaims: []loop.EvidenceClaim{{
						Claim:          "output-present",
						VerifierID:     "aegis.blob.present",
						PolicyVersion:  "v1",
						MediaType:      "text/plain",
						ExpectedDigest: "sha256:" + strings.Repeat("a", 64),
					}},
				},
				{
					ID:       "done",
					Kind:     loop.StepTerminal,
					Retry:    loop.RetryPolicy{MaxAttempts: 1},
					Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded},
				},
			},
			Transitions: []loop.Transition{{
				ID:         "t1",
				FromStepID: "act",
				ToStepID:   "done",
			}},
			RequiredEvidence: []loop.EvidenceRequirement{{
				Claim:          "output-present",
				ProducerStepID: "act",
			}},
		})
		if e != nil {
			t.Fatal(e)
		}
		return rev
	}

	okLoop := makeActionLoop("loop-lane-ok", "test", "secondary-model")
	if _, err := s.PublishLoopAs(ctx, subject, PublishLoopInput{AgentID: refs[0].ID, Revision: okLoop, IdempotencyKey: "pub-loop-ok"}); err != nil {
		t.Fatalf("expected authorized step lane loop to publish: %v", err)
	}
	badLoop := makeActionLoop("loop-lane-bad", "test", "rogue-model")
	if _, err := s.PublishLoopAs(ctx, subject, PublishLoopInput{AgentID: refs[0].ID, Revision: badLoop, IdempotencyKey: "pub-loop-bad"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("expected unauthorized step lane loop to be denied, got %v", err)
	}

	defaultLoop := makeActionLoop("loop-lane-default", "", "")
	if _, err := s.PublishLoopAs(ctx, subject, PublishLoopInput{AgentID: refs[0].ID, Revision: defaultLoop, IdempotencyKey: "pub-loop-default"}); err != nil {
		t.Fatal(err)
	}
	loopRef := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: defaultLoop.LoopID, Revision: 1, Digest: defaultLoop.Digest}
	if _, err := s.SetLoopLifecycleAs(ctx, subject, defaultLoop.LoopID, SetLoopLifecycleInput{AgentID: refs[0].ID, Loop: loopRef, State: loop.LifecycleActive, EventID: "act-loop-default"}); err != nil {
		t.Fatal(err)
	}

	makeGraph := func(graphID, provider, model string) graph.GraphRevision {
		g, _, e := graph.NewRevision(graph.GraphRevision{
			SchemaVersion: graph.RevisionSchemaVersion,
			GraphID:       graphID,
			Revision:      1,
			Nodes: []graph.Node{{
				ID:          "n1",
				Loop:        loopRef,
				Participant: refs[0],
				Provider:    provider,
				Model:       model,
			}},
		})
		if e != nil {
			t.Fatal(e)
		}
		return g
	}

	okGraph := makeGraph("graph-lane-ok", "test", "secondary-model")
	if _, err := s.PublishGraphAs(ctx, subject, PublishGraphInput{AgentID: refs[0].ID, Revision: okGraph, IdempotencyKey: "pub-graph-ok"}); err != nil {
		t.Fatalf("expected authorized node lane graph to publish: %v", err)
	}
	badGraph := makeGraph("graph-lane-bad", "test", "rogue-model")
	if _, err := s.PublishGraphAs(ctx, subject, PublishGraphInput{AgentID: refs[0].ID, Revision: badGraph, IdempotencyKey: "pub-graph-bad"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("expected unauthorized node lane graph to be denied, got %v", err)
	}
}
