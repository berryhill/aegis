package badger

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	badgerdb "github.com/dgraph-io/badger/v4"
)

// All positive records use normal publication/admission/control writers. This
// fixture deliberately stops before Delivery and Publication (separate families).
func recoveryContributionFixture(t *testing.T, preferencesFirst ...bool) (*Store, contribution.Contract) {
	t.Helper()
	return recoveryContributionFixtureStore(t, newDeliveryStore(t), preferencesFirst...)
}

func recoveryContributionFixtureStore(t *testing.T, s *Store, preferencesFirst ...bool) (*Store, contribution.Contract) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	registration, agent := agentFixture(t)
	_, err := s.RegisterAgent(ctx, registration, agent, audit("agent.registered", agent.AgentID))
	must(err)
	ref := registration.InitialRevision
	draft := loop.ImplementationDraft("Repair behavior", "Focused tests pass")
	draft.Workspace = "/owned/worktree"
	draft.WritableFiles = []string{"value.go"}
	draft.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestValue"}}
	profile := loop.ContributionProfile{Controller: ref, Builder: ref, Tester: ref, Repository: "owner/repo", RepositoryID: 7, Base: strings.Repeat("b", 40), ReadAction: contribution.ReadIssueAction, Effect: "local-only", DeadlineSeconds: 60, Invariants: []string{"preserve history"}, Exclusions: []string{"no publication"}}
	lr, lv, err := loop.NewContributionRevision("recovery-contribution", 1, "", draft, profile)
	if err != nil {
		t.Fatalf("contribution revision: %v %+v", err, lv.Issues)
	}
	_, err = s.PublishLoop(ctx, loop.PublishRequest{Revision: lr, Validation: lv, Provenance: loopPublicationProvenance(t, lr, lv), IdempotencyKey: "contribution-loop"}, audit("loop.published", lr.LoopID))
	must(err)
	lrRef := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: lr.LoopID, Revision: 1, Digest: lr.Digest}
	node := graph.Node{ID: "contribution", Participant: ref, Loop: lrRef}
	for _, p := range lr.Inputs {
		node.Inputs = append(node.Inputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	for _, p := range lr.Outputs {
		node.Outputs = append(node.Outputs, graph.Port{ID: p.ID, Type: graph.ValueType(p.Type), Required: p.Required})
	}
	nested, err := graph.NewDirectLoopSnapshot("contribution-snapshot", node, nil)
	must(err)
	snapshot, err := graph.NewDirectRunSnapshot(nested)
	must(err)
	now := time.Now().UTC()
	authority := lifecycleDigestRef("authority", contribution.Digest([]byte("authority")))
	sub, err := queue.NewSubmission(queue.Submission{SubmissionID: "contribution-submission", IdempotencyKey: "contribution-request", Snapshot: lifecycleDigestRef(snapshot.SnapshotID, snapshot.Digest), Authority: authority, MandateID: "mandate", Runtime: "hermes-agent", OwnerID: agent.Ownership.OwnerID, OwnerAgentID: agent.AgentID, SubmittedAt: now})
	must(err)
	item, err := queue.NewItem(queue.Item{ItemID: "contribution-item", Submission: lifecycleDigestRef(sub.SubmissionID, sub.Digest), Snapshot: sub.Snapshot, Authority: authority, GraphRunID: "contribution-run", MaxAttempts: 1, EnqueuedAt: now, AvailableAt: now})
	must(err)
	run, err := execution.NewGraphRun(execution.GraphRun{GraphRunID: item.GraphRunID, QueueItem: lifecycleDigestRef(item.ItemID, item.Digest), Snapshot: item.Snapshot, Authority: authority, CreatedAt: now})
	must(err)
	tr, err := queue.NewTransition(queue.QueueTransition{TransitionID: "contribution-queued", QueueItemID: item.ItemID, To: queue.StateQueued, Reason: "accepted", OccurredAt: now})
	must(err)
	issue, err := contribution.ParseIssueURL("https://github.com/owner/repo/issues/1")
	must(err)
	td, err := draft.Digest()
	must(err)
	role := contribution.RoleBinding{Agent: ref, Charter: agent.Charter, Runtime: agent.Runtime}
	c, err := contribution.Seal(contribution.Contract{SchemaVersion: contribution.Schema, OwnerID: sub.OwnerID, SessionID: "intake", Key: "contribution-key", PayloadDigest: contribution.Payload(lrRef, issue.URL, ""), QueueItemID: item.ItemID, SubmissionID: sub.SubmissionID, RunID: run.GraphRunID, Loop: lrRef, Controller: ref, Builder: ref, Tester: ref, Source: contribution.Source{URL: issue, RepositoryID: 7, IssueID: 9, Author: contribution.Author{ID: 1, Login: "owner"}, State: "open", CreatedAt: now, UpdatedAt: now, ObservedAt: now, ContentDigest: contribution.Digest([]byte("issue")), Discussion: []contribution.Comment{}}, Outcome: draft.Task, Acceptance: draft.Acceptance, Invariants: profile.Invariants, Exclusions: profile.Exclusions, Repository: profile.Repository, Base: profile.Base, Worktree: draft.Workspace, Effect: profile.Effect, Roles: []contribution.RoleBinding{role, role, role}, TemplateDigest: td, CandidateBudget: draft.MaxPasses, AdmittedAt: now, Deadline: now.Add(time.Minute)})
	must(err)
	_, err = s.AcceptSubmission(ctx, fleet.AcceptedSubmission{Snapshot: snapshot, Submission: sub, QueueItem: item, GraphRun: run, InitialTransition: tr, Contribution: &c}, audit("submission.accepted", sub.SubmissionID))
	must(err)
	if len(preferencesFirst) > 0 && preferencesFirst[0] {
		writeRecoverySharedPreferences(t, s, c)
	}
	must(s.AppendContributionControl(ctx, contribution.Control{QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: "control", Sequence: 1, Delivery: "local-only", At: now}))
	return s, c
}

func writeRecoverySharedPreferences(t *testing.T, s *Store, c contribution.Contract) {
	t.Helper()
	v, err := queue.NewPreferences(queue.Preferences{PrincipalID: c.QueueItemID, SubjectID: "operator", RequestID: "shared-preferences", Selection: queue.SelectionAll, Visibility: queue.VisibilityAll, Revision: 1, UpdatedAt: c.AdmittedAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.SetQueuePreferences(context.Background(), v, 0); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryControlPreferencesSharedPrefix(t *testing.T) {
	for _, first := range []bool{false, true} {
		name := "control-first"
		if first {
			name = "preferences-first"
		}
		t.Run(name, func(t *testing.T) {
			s, c := recoveryContributionFixture(t, first)
			if !first {
				writeRecoverySharedPreferences(t, s, c)
			}
			checkPresentationRecovery(t, s, true)
			prior, err := s.GetContributionControl(context.Background(), c.QueueItemID)
			if err != nil {
				t.Fatal(err)
			}
			err = s.AppendContributionControl(context.Background(), contribution.Control{QueueItemID: c.QueueItemID, ContractDigest: c.Digest, OwnerID: c.OwnerID, SessionID: "control", Sequence: 2, PreviousDigest: prior.Digest, Delivery: "local-only", Stop: true, At: c.AdmittedAt})
			if err != nil {
				t.Fatal(err)
			}
			checkPresentationRecovery(t, s, true)
			if err = s.update(context.Background(), func(tx *badgerdb.Txn) error { return tx.Delete(key(familyQueuePreferencesHead, c.QueueItemID)) }); err != nil {
				t.Fatal(err)
			}
			checkPresentationRecovery(t, s, false)
			if _, err = s.GetContributionControl(context.Background(), c.QueueItemID); err == nil {
				t.Fatal("orphan preferences ignored by control reader")
			}
		})
	}
}

func TestRecoveryContributionPresentationAliases(t *testing.T) {
	for _, mutation := range []string{"normal", "request-orphan", "request-key", "issue-key", "control-key", "control-head-missing", "control-history-missing"} {
		t.Run(mutation, func(t *testing.T) {
			s, c := recoveryContributionFixture(t)
			checkPresentationRecovery(t, s, true)
			if mutation == "normal" {
				return
			}
			err := s.update(context.Background(), func(tx *badgerdb.Txn) error {
				switch mutation {
				case "request-orphan":
					return tx.Set(key(familyContributionRequest, c.OwnerID, "orphan"), []byte("missing"))
				case "request-key":
					return tx.Set(key(familyContributionRequest, c.OwnerID, "wrong"), []byte(c.QueueItemID))
				case "issue-key":
					return tx.Set(key(familyContributionIssue, "7", "10"), []byte(c.QueueItemID))
				case "control-key":
					b, e := get(tx, key(familyContributionControl, c.QueueItemID, "1"))
					if e != nil {
						return e
					}
					return tx.Set(key(familyContributionControl, c.QueueItemID, "2"), b)
				case "control-head-missing":
					return tx.Delete(key(familyContributionControl, c.QueueItemID, "head"))
				case "control-history-missing":
					return tx.Delete(key(familyContributionControl, c.QueueItemID, "1"))
				}
				return fleet.ErrCorrupt
			})
			if err != nil {
				t.Fatal(err)
			}
			checkPresentationRecovery(t, s, false)
		})
	}
}
