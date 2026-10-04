package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/continuation"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
)

func continuationFixture(t *testing.T) (*app.Service, core.Subject, app.DoerContinuationIntent) {
	t.Helper()
	svc, controller, candidateInput := candidateReadinessFixture(t, "none")
	now := controller.AuthenticatedAt
	svc.Now = func() time.Time { return now }
	svc.Config.Credentials.Authority.DeploymentID = "continuation-deployment"
	requester := core.Subject{ID: "password:" + strings.Repeat("b", 64), Kind: "principal", PrincipalID: svc.Config.Principal.ID, Issuer: "aegis-principal-auth", Method: "password", AuthenticatedAt: now, ExpiresAt: now.Add(time.Hour)}
	candidate := candidateInput.Candidate
	draft, err := svc.SaveDoerDraftAs(context.Background(), requester, app.DoerDraftInput{Agent: candidateInput.Agent, LoopID: candidate.LoopID, Revision: candidate.Revision, Contract: *candidate.Doer})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := svc.FleetRepository.LatestAgentRevision(context.Background(), candidateInput.Agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: candidate.LoopID, Revision: candidate.Revision, Digest: candidate.Digest}
	intent := app.DoerContinuationIntent{DraftID: draft.ID, DraftVersion: draft.Version, CandidateDigest: candidate.Digest, Agent: candidateInput.Agent, Loop: ref, Charter: agent.Charter, PublicationKey: draft.PublicationKey, Run: app.QueueLoopInput{Agent: candidateInput.Agent, Loop: ref, IdempotencyKey: "original-explicit-run-key", Activate: true}, Requester: requester, SessionID: "original-trusted-session-id", DeploymentID: svc.Config.Credentials.Authority.DeploymentID, ConfigIdentity: svc.DoerContinuationConfigIdentity(), ExpiresAt: now.Add(30 * time.Second)}
	return svc, controller, intent
}

func continuationState(t *testing.T, svc *app.Service) map[string][32]byte {
	t.Helper()
	root := svc.Store.Root()
	result := map[string][32]byte{}
	// Verify canonical facts, not sparse mutable Badger engine files.
	capture := func(value any, err error) [32]byte {
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(wire)
	}
	ctx := context.Background()
	result["canonical/loops"] = capture(svc.FleetRepository.ListLoopRevisions(ctx))
	result["canonical/lifecycle"] = capture(svc.FleetRepository.ListLoopLifecycleEvents(ctx))
	result["canonical/graphs"] = capture(svc.FleetRepository.ListGraphRevisions(ctx))
	result["canonical/submissions"] = capture(svc.FleetRepository.ListSubmissions(ctx))
	result["canonical/rejections"] = capture(svc.FleetRepository.ListRejections(ctx))
	result["canonical/queue"] = capture(svc.FleetRepository.ListQueueItems(ctx))
	result["canonical/claims"] = capture(svc.FleetRepository.ListClaims(ctx))
	result["canonical/attempts"] = capture(svc.FleetRepository.ListAttempts(ctx))
	result["canonical/graph-runs"] = capture(svc.FleetRepository.ListGraphRuns(ctx))
	result["canonical/loop-executions"] = capture(svc.FleetRepository.ListLoopExecutions(ctx))
	result["canonical/mandates"] = capture(svc.Authority.ListMandates(ctx))
	result["canonical/contexts"] = capture(svc.Authority.ListAuthorityContexts(ctx))
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == filepath.Join(root, "persistence") {
				return filepath.SkipDir
			}
			return nil
		}
		wire, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[name] = sha256.Sum256(wire)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDoerContinuationSignedApprovalResolveAndQueueGate(t *testing.T) {
	svc, controller, intent := continuationFixture(t)
	ctx := context.Background()
	beforeItems, err := svc.ListQueueAs(ctx, controller)
	if err != nil {
		t.Fatal(err)
	}
	id, err := svc.ApproveDoerContinuationAs(ctx, controller, intent)
	if err != nil || id == "" {
		t.Fatalf("approval = %q %v", id, err)
	}
	again, err := svc.ApproveDoerContinuationAs(ctx, controller, intent)
	if err != nil || again != id {
		t.Fatalf("replay = %q %v", again, err)
	}
	approved, err := svc.Store.ReadDoerContinuation(id)
	if err != nil {
		t.Fatal(err)
	}
	key, err := svc.Store.DoerContinuationPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	if err = continuation.Verify(approved, key); err != nil {
		t.Fatal(err)
	}
	executor, err := svc.ResolveDoerContinuationAs(ctx, intent.Requester, intent.SessionID, id, intent)
	if err != nil || !reflect.DeepEqual(executor, controller) {
		t.Fatalf("resolve = %#v %v", executor, err)
	}
	items, err := svc.ListQueueAs(ctx, controller)
	if err != nil || !reflect.DeepEqual(items, beforeItems) {
		t.Fatal("approval queued work")
	}
	draft, err := svc.ReadDoerDraftAs(ctx, intent.Requester, intent.DraftID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, _, err := loop.NewDoerRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PublishLoopAs(ctx, controller, app.PublishLoopInput{AgentID: intent.Agent.ID, Revision: candidate, IdempotencyKey: intent.PublicationKey}); err != nil {
		t.Fatal(err)
	}
	var request string
	for attempt := 0; attempt < 2; attempt++ {
		result, err := svc.QueueDoerContinuationAs(ctx, intent.Requester, intent.SessionID, id, intent)
		if err != nil || result.Reason != "doer_model_required" || result.QueueItemID != "" || result.Execution != nil {
			t.Fatalf("queue gate = %+v %v", result, err)
		}
		if request != "" && request != result.RequestID {
			t.Fatal("reminted execution key")
		}
		request = result.RequestID
	}
	var reservation string
	if err = svc.Store.Load("queue-loop-intent", request, &reservation); err != nil || reservation == "" {
		t.Fatalf("missing original queue fence: %v", err)
	}
	items, err = svc.ListQueueAs(ctx, controller)
	if err != nil || len(items) != 0 {
		t.Fatal("prerequisite denial admitted work")
	}
	graphs, err := svc.ListGraphsAs(ctx, controller)
	if err != nil || len(graphs) != 0 {
		t.Fatal("prerequisite denial published graph")
	}
	if _, err = os.Stat(filepath.Join(candidate.Doer.Workspace, candidate.Doer.VerifyFile)); !os.IsNotExist(err) {
		t.Fatal("runtime effect before host scope")
	}
	outcomes := 0
	err = svc.Store.List("doer-continuation-outcomes", func(json.RawMessage) error { outcomes++; return nil })
	if err != nil || outcomes != 1 {
		t.Fatalf("outcome linkage: %d %v", outcomes, err)
	}
	events, err := svc.Store.AuditEvents()
	if err != nil {
		t.Fatal(err)
	}
	requesterAudit, executorAudit := false, false
	for _, event := range events {
		if event.Type == "doer_continuation_request" && event.SubjectID == intent.Requester.ID {
			requesterAudit = true
		}
		if event.Type == "doer_continuation_executor" && event.SubjectID == controller.ID {
			executorAudit = true
		}
	}
	if !requesterAudit || !executorAudit {
		t.Fatal("lineage not separately audited")
	}
}

func TestDoerContinuationControllerAuthenticationDenials(t *testing.T) {
	tests := map[string]func(*core.Subject){
		"password with forged UID and issuer": func(s *core.Subject) { s.Method = "password" },
		"wrong issuer":                        func(s *core.Subject) { s.Issuer = "local-os" },
		"wrong principal":                     func(s *core.Subject) { s.PrincipalID = "other" },
		"wrong UID":                           func(s *core.Subject) { s.Claims["uid"] = "0" },
		"wrong subject":                       func(s *core.Subject) { s.ID = "local-uid:0" },
		"nonhuman":                            func(s *core.Subject) { s.Kind = "agent" },
		"future authentication":               func(s *core.Subject) { s.AuthenticatedAt = s.AuthenticatedAt.Add(time.Second) },
		"expired":                             func(s *core.Subject) { s.ExpiresAt = s.AuthenticatedAt },
		"freshness boundary":                  func(s *core.Subject) { s.AuthenticatedAt = s.AuthenticatedAt.Add(-time.Minute) },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			svc, controller, intent := continuationFixture(t)
			before := continuationState(t, svc)
			change(&controller)
			id, err := svc.ApproveDoerContinuationAs(context.Background(), controller, intent)
			if id != "" || !errors.Is(err, app.ErrDenied) {
				t.Fatalf("approval = %q, %v", id, err)
			}
			if !reflect.DeepEqual(before, continuationState(t, svc)) {
				t.Fatal("denial mutated state")
			}
		})
	}
}

func TestDoerContinuationExactIntentDenials(t *testing.T) {
	tests := map[string]func(*app.DoerContinuationIntent){
		"draft version":             func(i *app.DoerContinuationIntent) { i.DraftVersion++ },
		"draft ID":                  func(i *app.DoerContinuationIntent) { i.DraftID = "invalid-draft" },
		"candidate digest":          func(i *app.DoerContinuationIntent) { i.CandidateDigest = "sha256:" + strings.Repeat("f", 64) },
		"Agent":                     func(i *app.DoerContinuationIntent) { i.Agent.Revision++; i.Run.Agent = i.Agent },
		"Loop":                      func(i *app.DoerContinuationIntent) { i.Loop.Revision++; i.Run.Loop = i.Loop },
		"publication key":           func(i *app.DoerContinuationIntent) { i.PublicationKey = "replacement-key" },
		"deployment":                func(i *app.DoerContinuationIntent) { i.DeploymentID = "other" },
		"missing config identity":   func(i *app.DoerContinuationIntent) { i.ConfigIdentity = "" },
		"missing execution key":     func(i *app.DoerContinuationIntent) { i.Run.IdempotencyKey = "" },
		"substituted Run Agent":     func(i *app.DoerContinuationIntent) { i.Run.Agent.Revision++ },
		"expired approval":          func(i *app.DoerContinuationIntent) { i.ExpiresAt = i.Requester.AuthenticatedAt },
		"widened controller expiry": func(i *app.DoerContinuationIntent) { i.ExpiresAt = i.Requester.AuthenticatedAt.Add(61 * time.Second) },
		"wrong requester principal": func(i *app.DoerContinuationIntent) { i.Requester.PrincipalID = "other" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			svc, controller, intent := continuationFixture(t)
			before := continuationState(t, svc)
			change(&intent)
			id, err := svc.ApproveDoerContinuationAs(context.Background(), controller, intent)
			if id != "" || err == nil || errors.Is(err, app.ErrDoerContinuationUnavailable) {
				t.Fatalf("invalid intent reached substrate: %q, %v", id, err)
			}
			if !reflect.DeepEqual(before, continuationState(t, svc)) {
				t.Fatal("invalid intent mutated state")
			}
		})
	}
}

func TestDoerContinuationResolveRequesterSessionEquality(t *testing.T) {
	for _, field := range []string{"session", "subject", "issuer", "auth_time", "principal"} {
		t.Run(field, func(t *testing.T) {
			svc, _, intent := continuationFixture(t)
			before := continuationState(t, svc)
			requester, session := intent.Requester, intent.SessionID
			switch field {
			case "session":
				session = "other-session"
			case "subject":
				requester.ID = "other-browser-subject"
			case "issuer":
				requester.Issuer = "linux-so-peercred"
			case "auth_time":
				requester.AuthenticatedAt = requester.AuthenticatedAt.Add(-time.Second)
			case "principal":
				requester.PrincipalID = "other-principal"
			}
			executor, err := svc.ResolveDoerContinuationAs(context.Background(), requester, session, "unverified-record", intent)
			if !errors.Is(err, app.ErrDenied) || !reflect.DeepEqual(executor, core.Subject{}) {
				t.Fatalf("resolve = %#v, %v", executor, err)
			}
			if !reflect.DeepEqual(before, continuationState(t, svc)) {
				t.Fatal("resolve denial mutated state")
			}
		})
	}
}

func TestDoerContinuationDraftChangeAndCancellationFailClosed(t *testing.T) {
	svc, controller, intent := continuationFixture(t)
	draft, err := svc.ReadDoerDraftAs(context.Background(), intent.Requester, intent.DraftID)
	if err != nil {
		t.Fatal(err)
	}
	contract := draft.Contract
	contract.Task = "Changed after review"
	if _, err := svc.SaveDoerDraftAs(context.Background(), intent.Requester, app.DoerDraftInput{ID: draft.ID, ExpectedVersion: draft.Version, Agent: draft.Agent, LoopID: draft.LoopID, Revision: draft.Revision, Contract: contract}); err != nil {
		t.Fatal(err)
	}
	before := continuationState(t, svc)
	if _, err := svc.ApproveDoerContinuationAs(context.Background(), controller, intent); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("edited draft = %v", err)
	}
	if _, err := svc.ResolveDoerContinuationAs(context.Background(), intent.Requester, intent.SessionID, "unverified-record", intent); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("edited draft resolve = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.ApproveDoerContinuationAs(ctx, controller, intent); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled approval = %v", err)
	}
	if _, err := svc.QueueDoerContinuationAs(ctx, intent.Requester, intent.SessionID, "unverified-record", intent); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled queue = %v", err)
	}
	if _, err := svc.ApproveDoerContinuationAs(nil, controller, intent); !errors.Is(err, app.ErrDenied) {
		t.Fatalf("nil context = %v", err)
	}
	if !reflect.DeepEqual(before, continuationState(t, svc)) {
		t.Fatal("edited-draft/cancelled denial mutated state")
	}
}

func TestDoerContinuationConcurrentSameKeySubstitutionFailsClosed(t *testing.T) {
	svc, controller, intent := continuationFixture(t)
	id, err := svc.ApproveDoerContinuationAs(context.Background(), controller, intent)
	if err != nil {
		t.Fatal(err)
	}
	before := continuationState(t, svc)
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			changed := intent
			changed.Run.Inputs = []graph.NormalizedInput{{PortID: "input", Value: json.RawMessage(`{"changed":true}`)}}
			if _, err := svc.ApproveDoerContinuationAs(context.Background(), controller, changed); !errors.Is(err, app.ErrConflict) {
				t.Errorf("substitution approval: %v", err)
			}
			if _, err := svc.QueueDoerContinuationAs(context.Background(), changed.Requester, changed.SessionID, id, changed); !errors.Is(err, app.ErrConflict) {
				t.Errorf("substitution queue: %v", err)
			}
		}()
	}
	wg.Wait()
	if !reflect.DeepEqual(before, continuationState(t, svc)) {
		t.Fatal("substitution mutated state")
	}
}
