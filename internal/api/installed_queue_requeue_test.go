package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/queue"
	"github.com/berryhill/aegis/internal/reference"
	consoleweb "github.com/berryhill/aegis/web/console"
	"github.com/labstack/echo/v5"
)

func TestPersistedRequeueEntrypointSuccess(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "linked-requeue-success")
}
func TestPersistedRequeueEntrypointExhaustion(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "linked-requeue-failure")
}

func TestConsoleQueueSupportedFailedDoerRequeue(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		installedOnlineDoerExactBytesSuccess(t, true, "linked-requeue-console-success")
	})
	t.Run("exhaustion", func(t *testing.T) {
		installedOnlineDoerExactBytesSuccess(t, true, "linked-requeue-console-failure")
	})
}

func TestConsoleQueueMismatchedAuthenticationRequeue(t *testing.T) {
	installedOnlineDoerExactBytesSuccess(t, true, "linked-requeue-console-mismatched-authentication")
}

// Real service, Badger, worker and verifier; synthetic tool-free runtime only.
// Initial failure uses the existing trusted CLI fixture. Every successor request
// calls the new authenticated service entrypoint, not QueueLoop or a helper.
func installedPersistedRequeue(t *testing.T, ctx context.Context, s *app.Service, cli *installedLoopQueueClient, input app.QueueLoopInput, workspace, install, script string, secondFailure, consoleMode, mismatchedAuthentication bool) {
	t.Helper()
	python := filepath.Join(install, "venv", "bin", "python")
	failure := strings.ReplaceAll(script, `\"edits\"`, `\"unknown_edits\"`)
	if failure == script {
		t.Fatal("missing malformed-proposal substitution")
	}
	if err := os.WriteFile(python, []byte(failure), 0700); err != nil {
		t.Fatal(err)
	}
	// Shorten only the initial Run's signed host scope, not the browser's
	// configured freshness policy. Restoring the policy cannot extend the
	// already signed grant; it still expires during the real wait below.
	principalAuthTTL := s.Config.Principal.AuthTTL
	s.Config.Principal.AuthTTL = 10 * time.Second
	_, _ = cli.queue(input) // failure may be returned after durable disposition
	s.Config.Principal.AuthTTL = principalAuthTTL
	sub, err := s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.ListQueueAs(ctx, sub)
	if err != nil || len(items) != 1 {
		t.Fatalf("initial failure readback: %d %v", len(items), err)
	}
	old := items[0]
	if old.Projection.State != queue.StateFailed || old.Disposition == nil || old.Disposition.ReasonCode != "doer_loop_failed" || old.Artifact != nil || len(old.Receipts) != 0 || len(old.Attempts) != 1 {
		t.Fatalf("not a clean durable failure: %+v", old)
	}
	selector := app.RequeueQueueItemInput{QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: old.Item.ItemID, Digest: old.Item.Digest}, IdempotencyKey: "persisted-requeue"}
	eligibility, err := s.CheckQueueRequeueAs(ctx, sub, selector.QueueItem)
	if err != nil || eligibility.Reason != "eligible_fresh_admission_required" || eligibility.Execution != nil || eligibility.Reexecution != nil {
		t.Fatalf("fresh OS read-only eligibility: %+v %v", eligibility, err)
	}
	var eligibilitySlot app.LoopReexecutionFact
	if err := s.Store.Load("queue-loop-reexecution-successor", old.Item.ItemID, &eligibilitySlot); !os.IsNotExist(err) {
		t.Fatal("read-only eligibility reserved successor", err)
	}
	consolePost := func() {}
	var nativeExecutor core.Subject
	prepareConsole := func() {
		manager, cookie, csrf := consoleQueueRouteFixture(t, s)
		e := echo.New()
		e.HTTPErrorHandler = func(c *echo.Context, err error) {
			status, _, _ := classifyError(err)
			_ = c.String(status, err.Error())
		}
		portal := NewDoerContinuationPortal(s, manager)
		portal.RegisterBrowser(e)
		registerConsoleQueuePresentation(e, s, manager, portal)
		// Read back under the same password-session subject used by the route.
		// The fixture must not synthesize the server's idempotency intent.
		authRequest := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/queue/"+old.Item.ItemID+"/requeue", nil)
		authRequest.Header.Set("Origin", "http://127.0.0.1")
		authRequest.Header.Set("X-CSRF-Token", csrf)
		authRequest.AddCookie(cookie)
		sub, err = manager.AuthorizeMutation(authRequest)
		if err != nil {
			t.Fatal(err)
		}
		model := consoleweb.SurfaceModel{Domain: "queue", CSRF: csrf, Records: []consoleweb.RecordModel{consoleQueueRecord(old)}}
		applyConsoleQueuePresentation(ctx, s, sub, &model)
		record := model.Records[0]
		if record.QueueActions == nil {
			t.Fatal("missing canonical requeue eligibility")
		}
		if mismatchedAuthentication {
			if record.QueueActions.CanRequeue || !strings.Contains(record.QueueActions.RequeueReason, "session_selection_zero_authorized_matches") || !strings.Contains(record.QueueActions.RequeueReason, "independent_setup_required") {
				t.Fatalf("render must expose incompatible authentication without Requeue control: %+v", record.QueueActions)
			}
		} else if !record.QueueActions.CanReviewRequeue {
			t.Fatalf("supported Console-positive execution path unavailable: %s", record.QueueActions.RequeueReason)
		}
		for _, component := range []func() ([]byte, error){
			func() ([]byte, error) { return renderConsole(ctx, consoleweb.QueueRecordPanel(model, model.Records)) },
			func() ([]byte, error) {
				return renderConsole(ctx, consoleweb.QueueWorkspace(model, &record, consoleweb.QueuePinnedTopology{}))
			},
		} {
			body, err := component()
			if err != nil {
				t.Fatal(err)
			}
			hasForm := strings.Contains(string(body), "/"+old.Item.ItemID+"/requeue\"")
			if !mismatchedAuthentication {
				hasForm = strings.Contains(string(body), "/"+old.Item.ItemID+"/requeue-review")
			}
			if mismatchedAuthentication {
				if hasForm || !strings.Contains(string(body), "session_selection_zero_authorized_matches") {
					t.Fatalf("render hid authentication eligibility or offered an unsupported form: %s", body)
				}
			} else if !hasForm {
				t.Fatalf("missing rendered requeue form: %s", body)
			}
		}
		if !mismatchedAuthentication {
			values := url.Values{"csrf": {csrf}, "digest": {old.Item.Digest}, "expected_revision": {"0"}}
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/queue/"+old.Item.ItemID+"/requeue-review", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "http://127.0.0.1")
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("requeue review: %d %s", rec.Code, rec.Body.String())
			}
			if err := s.Store.Load("queue-loop-reexecution-successor", old.Item.ItemID, &eligibilitySlot); !os.IsNotExist(err) {
				t.Fatal("review reserved successor", err)
			}
			var pending doerPending
			for _, v := range portal.pending {
				pending = *v
			}
			if pending.ID == "" || pending.Review.Intent.Run.Reexecution == nil || pending.Review.Published == nil {
				t.Fatal("missing predecessor native review")
			}
			// A normal Run-shaped intent can never reuse this approval.
			portal.launch = func(ctx context.Context, id string, bind func(int)) string {
				// The only mocked boundary is independent human confirmation.
				approveRequeueNativeFixture(t, portal, id)
				return "approved"
			}
			reviewed := portalPost(e, cookie, csrf, url.Values{"action": {"request-local-review"}, "intent_id": {pending.ID}})
			if reviewed.Code != 200 || !strings.Contains(reviewed.Body.String(), "Requeue exact approved predecessor") {
				t.Fatalf("native continuation browser return: %d %s", reviewed.Code, reviewed.Body.String())
			}
			if err := s.Store.Load("queue-loop-reexecution-successor", old.Item.ItemID, &eligibilitySlot); !os.IsNotExist(err) {
				t.Fatal("native approval reserved successor", err)
			}
			_, session, err := manager.AuthorizeCommand(authRequest)
			if err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"ordinary-run", "changed-key", "changed-budget", "changed-source", "changed-session"} {
				altered := pending.Review.Intent
				alteredSession := session
				switch kind {
				case "ordinary-run":
					altered.Run.Reexecution = nil
					altered.Run.Activate = true
				case "changed-key":
					altered.Run.IdempotencyKey += "-other"
				case "changed-budget":
					altered.RequeueBinding = "sha256:" + strings.Repeat("f", 64)
				case "changed-source":
					reexecution := *altered.Run.Reexecution
					reexecution.PreviousQueue.Digest = "sha256:" + strings.Repeat("f", 64)
					altered.Run.Reexecution = &reexecution
				case "changed-session":
					alteredSession = "other-browser-session"
				}
				if _, err := s.ResolveDoerContinuationAs(ctx, sub, alteredSession, portal.pending[pending.ID].ApprovalID, altered); err == nil {
					t.Fatal("altered native requeue approval accepted", kind)
				}
			}
			approved, err := s.Store.ReadDoerContinuation(portal.pending[pending.ID].ApprovalID)
			if err != nil {
				t.Fatal(err)
			}
			// Preserve the signed original executor for successor-owner assertions.
			nativeExecutor = approved.Executor
		}
		consolePost = func() {
			t.Helper()
			values := url.Values{"csrf": {csrf}, "digest": {old.Item.Digest}, "expected_revision": {"0"}}
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/console/queue/"+old.Item.ItemID+"/requeue", strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", "http://127.0.0.1")
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if mismatchedAuthentication {
				t.Logf("authenticated Console requeue: status=%d body=%q", rec.Code, rec.Body.String())
				if rec.Code != http.StatusOK || rec.Header().Get("Location") != "" || !strings.Contains(rec.Body.String(), "independent_setup_required") || !strings.Contains(rec.Body.String(), "session_selection_zero_authorized_matches") {
					t.Errorf("Console mismatch must expose independent_setup_required and session_selection_zero_authorized_matches without redirect: %d %s", rec.Code, rec.Body.String())
				}
				if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "<!doctype html>") || strings.Contains(rec.Body.String(), "style=") {
					t.Error("Console admission gate must be CSP-safe HTML, not a browser-generated text viewer")
				}
				return
			}
			if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/console/queue") {
				t.Fatalf("console requeue: %d %s", rec.Code, rec.Body.String())
			}
		}
	}
	assertUnchanged := func(want []app.QueueExecutionView) {
		t.Helper()
		got, err := s.ListQueueAs(ctx, sub)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("denial mutated durable queue", err)
		}
	}
	// Regress the policy boundary without depending on a race-instrumented
	// Run happening to finish inside ten seconds. This is read-only and cannot
	// create a successor or substitute for the browser's native approval.
	aged := sub
	aged.AuthenticatedAt = s.Now().Add(-11 * time.Second)
	if got, err := s.CheckQueueRequeueAs(ctx, aged, selector.QueueItem); err != nil || got.Reason != "eligible_fresh_admission_required" {
		t.Fatalf("initial host-scope TTL leaked into principal freshness: %+v %v", got, err)
	}
	for _, kind := range []string{"owner", "expired-local-os", "stale-password", "password-expiry-boundary", "digest"} {
		badSub, bad := sub, selector
		switch kind {
		case "owner":
			badSub.PrincipalID = "other-owner"
		case "expired-local-os":
			badSub.ExpiresAt = s.Now().Add(-time.Second)
		case "stale-password":
			badSub.Method = "password"
			badSub.AuthenticatedAt = s.Now().Add(-s.Config.Principal.AuthTTL - time.Second)
		case "password-expiry-boundary":
			badSub.Method = "password"
			badSub.AuthenticatedAt = s.Now().Add(-s.Config.Principal.AuthTTL)
		case "digest":
			bad.QueueItem.Digest = "sha256:" + strings.Repeat("f", 64)
		}
		if _, err := s.RequeueQueueItemAs(ctx, badSub, bad); err == nil {
			t.Fatalf("%s admitted", kind)
		}
		assertUnchanged(items)
	}
	var slot app.LoopReexecutionFact
	if err := s.Store.Load("queue-loop-reexecution-successor", old.Item.ItemID, &slot); !os.IsNotExist(err) {
		t.Fatal("denials reserved successor", err)
	}
	if mismatchedAuthentication {
		// Keep the canonical OS charter; a browser requester is not an OS executor.
		charter, err := s.GetCharter(input.Agent.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := core.ValidateCharter(charter.Charter); err != nil {
			t.Fatal("original fixture charter invalid", err)
		}
		unsupported := charter.Charter
		unsupported.Stanzas = append([]core.TrustStanza(nil), charter.Charter.Stanzas...)
		unsupported.Stanzas[0].Authentication.Methods = []string{"password"}
		if _, err := core.Canonicalize(unsupported); err == nil || !strings.Contains(err.Error(), "stanzas[0] contains an empty, wildcard, duplicate, or unsupported authentication method") {
			t.Fatalf("password canonical validation: %v", err)
		} else {
			t.Logf("unsupported password canonical validation: %v", err)
		}
		prepareConsole()
		if sub.Kind != "principal" || sub.Method != "password" || sub.Issuer != "aegis-principal-auth" {
			t.Fatal("fixture did not authenticate an actual Console password subject")
		}
		decision, err := s.Select(charter, sub, "", core.Environment{Name: "local"})
		if err == nil || decision.Reason != "zero_authorized_matches" || decision.MatchingCount != 0 {
			t.Fatalf("password unexpectedly selected OS stanza: %+v %v", decision, err)
		}
		consolePost()
		if os.Getenv("AEGIS_QUEUE_REQUEUE_BROWSER_TEST") == "1" {
			queueRequeueBrowser(t, s, old.Item.ItemID)
		}
		if err := s.Store.Load("queue-loop-reexecution-successor", old.Item.ItemID, &slot); !os.IsNotExist(err) {
			t.Errorf("mismatched authentication reserved successor: load=%v reserved=%t", err, err == nil)
		} else {
			t.Log("PASS: no persisted successor reservation")
		}
		assertUnchanged(items)
		t.Log("PASS: queue cardinality, attempts, evidence and predecessor terminal failure unchanged")
		if _, err := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
			t.Error("mismatched authentication produced output", err)
		} else {
			t.Log("PASS: no output")
		}
		return
	}
	if !consoleMode {
		// This substantive negative probe reserves the single successor. Keep
		// it in the persisted-entrypoint cases, never before a Console intent.
		blocked, err := s.RequeueQueueItemAs(ctx, sub, selector)
		if err != nil || blocked.Reexecution == nil || blocked.Reexecution.Remaining != 1 || blocked.Reason == "processed" || blocked.Execution != nil && len(blocked.Execution.Attempts) != 0 {
			t.Fatalf("old authority reused: %+v %v", blocked, err)
		}
	}
	authority, err := s.Authority.GetAuthorityContext(ctx, old.RuntimeAuthorityBinding.Authority.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeSessionAs(ctx, sub, authority.SessionID, "isolated_requeue_fixture"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10100 * time.Millisecond) // actual signed host scope expires
	if !secondFailure {
		if err := os.WriteFile(python, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	sub, err = s.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	var result app.QueueLoopResult
	var runErr error
	if consoleMode {
		// Authenticate the browser after the signed host scope has expired.
		// The original configured password freshness window still applies.
		prepareConsole()
		if err := s.Store.Load("queue-loop-reexecution-successor", old.Item.ItemID, &slot); !os.IsNotExist(err) {
			t.Fatal("Console predecessor already has a successor intent", err)
		}
		consolePost()
		if err := s.Store.Load("queue-loop-reexecution-successor", old.Item.ItemID, &slot); err != nil {
			t.Fatal(err)
		}
		if slot.SubjectID != nativeExecutor.ID || slot.PrincipalID != nativeExecutor.PrincipalID {
			t.Fatal("Console successor is not owned by the signed genuine native executor")
		}
		next, err := s.GetQueueItemAs(ctx, sub, slot.RequestID+"-queue")
		if err != nil {
			t.Fatal(err)
		}
		result = app.QueueLoopResult{RequestID: slot.RequestID, Execution: &next, Reexecution: &slot}
		consolePost() // same authenticated form must reconcile the linked successor
	} else {
		result, runErr = s.RequeueQueueItemAs(ctx, sub, selector)
		if runErr != nil && secondFailure {
			result, runErr = s.RequeueQueueItemAs(ctx, sub, selector)
		}
	}
	if runErr != nil || result.Execution == nil || result.Reexecution == nil {
		t.Fatalf("entrypoint execution: %+v %v", result, runErr)
	}
	next := *result.Execution
	expected := queue.StateSucceeded
	if secondFailure {
		expected = queue.StateFailed
	}
	if next.Projection.State != expected || next.Item.ItemID == old.Item.ItemID || next.Item.GraphRunID == old.Item.GraphRunID || len(next.Attempts) != 1 || next.RuntimeAuthorityBinding.Authority == old.RuntimeAuthorityBinding.Authority {
		t.Fatalf("successor identity/state mismatch: %+v", next)
	}
	if result.Reexecution.Remaining != 1 || result.Reexecution.Consumed != 1 {
		t.Fatal("budget reset")
	}
	var fact app.LoopReexecutionFact
	if err := s.Store.Load("queue-loop-reexecution-successor", old.Item.ItemID, &slot); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Load("queue-loop-reexecution", result.RequestID, &fact); err != nil {
		t.Fatal(err)
	}
	if slot != fact || fact != *result.Reexecution || fact.Previous.PreviousQueue != selector.QueueItem || fact.Agent != input.Agent || fact.Loop != input.Loop {
		t.Fatalf("durable linkage mismatch: %+v", fact)
	}
	oldSnapshot, err := s.FleetRepository.GetGraphRunSnapshot(ctx, old.Item.Snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	nextSnapshot, err := s.FleetRepository.GetGraphRunSnapshot(ctx, next.Item.Snapshot.ID)
	if err != nil || !reflect.DeepEqual(oldSnapshot.Inputs, nextSnapshot.Inputs) || !reflect.DeepEqual(oldSnapshot.Loops, nextSnapshot.Loops) || !reflect.DeepEqual(oldSnapshot.Participants, nextSnapshot.Participants) {
		t.Fatal("canonical task/input/pins changed", err)
	}
	if consoleMode {
		consolePost()
	} else {
		replay, err := s.RequeueQueueItemAs(ctx, sub, selector)
		if err != nil || !reflect.DeepEqual(replay.Execution, result.Execution) {
			t.Fatal("exact-key replay changed execution", err)
		}
	}
	var wg sync.WaitGroup
	for _, key := range []string{selector.IdempotencyKey, "distinct-a", "distinct-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			if consoleMode {
				consolePost()
				return
			}
			in := selector
			in.IdempotencyKey = key
			got, err := s.RequeueQueueItemAs(ctx, sub, in)
			if err == nil && (got.Execution == nil || got.Execution.Item.ItemID != next.Item.ItemID) {
				t.Errorf("concurrent call diverged: %+v", got)
			}
		}(key)
	}
	wg.Wait()
	current, err := s.ListQueueAs(ctx, sub)
	if err != nil || len(current) != 2 {
		t.Fatalf("durable successor cardinality: %d %v", len(current), err)
	}
	prior, err := s.GetQueueItemAs(ctx, sub, old.Item.ItemID)
	if err != nil || !reflect.DeepEqual(prior, old) {
		t.Fatal("predecessor terminal attempts/evidence changed", err)
	}
	retained, err := s.GetQueueItemAs(ctx, sub, next.Item.ItemID)
	if err != nil || !reflect.DeepEqual(retained, next) {
		t.Fatal("successor changed after replay/concurrency", err)
	}
	third := app.RequeueQueueItemInput{QueueItem: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: next.Item.ItemID, Digest: next.Item.Digest}, IdempotencyKey: "third-denied"}
	checkSubject := sub
	if consoleMode {
		checkSubject = nativeExecutor
	}
	denied, err := s.RequeueQueueItemAs(ctx, checkSubject, third)
	if err == nil {
		t.Fatal("exhausted/unsupported source admitted")
	}
	if secondFailure {
		if denied.Reason != "doer_reexecution_budget_exhausted" {
			t.Fatalf("wrong exhaustion denial: %+v %v", denied, err)
		}
		if next.Disposition == nil || next.Disposition.ReasonCode != "doer_loop_failed" || next.Artifact != nil || len(next.Receipts) != 0 {
			t.Fatal("exhaustion lacks failed disposition or retained success evidence")
		}
		var visits uint32
		for _, step := range next.DoerSteps[next.Attempts[0].AttemptID] {
			if step.StepID == "implement" && step.Visit > visits {
				visits = step.Visit
			}
		}
		if visits != 1 {
			t.Fatalf("remaining pass cap not enforced: %d", visits)
		}
		if _, err := os.Stat(filepath.Join(workspace, "result.txt")); !os.IsNotExist(err) {
			t.Fatal("failure wrote output", err)
		}
	} else {
		if !strings.Contains(err.Error(), "requeue_terminal_failure_required") {
			t.Fatalf("wrong unsupported denial: %v", err)
		}
		raw, err := os.ReadFile(filepath.Join(workspace, "result.txt"))
		if err != nil || string(raw) != "hello" || next.Artifact == nil || len(next.Receipts) != 1 {
			t.Fatal("success lacks selected-file evidence", err)
		}
		artifact, receipt, terminal := next.Artifact, next.Receipts[0], next.Disposition
		itemRef := reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: next.Item.ItemID, Digest: next.Item.Digest}
		if terminal == nil || len(next.LoopExecutions) != 1 || len(next.Claims) != 1 {
			t.Fatal("success lacks terminal/Loop/claim evidence")
		}
		attempt, child := next.Attempts[0], next.LoopExecutions[0]
		if artifact.Validate() != nil || receipt.Validate() != nil || receipt.Outcome != evidence.Passed || receipt.ArtifactID != artifact.ID || receipt.VerifierID == "" || artifact.AttemptID != attempt.AttemptID || artifact.RunID != child.LoopExecutionID || artifact.OwnerID != input.Agent.ID || artifact.ActionID != "verify" || receipt.AttemptID != artifact.AttemptID || receipt.RunID != artifact.RunID || receipt.OwnerID != artifact.OwnerID || receipt.ActionID != artifact.ActionID || receipt.AuthorityContextID != artifact.AuthorityContextID || receipt.AuthorityContextDigest != artifact.AuthorityContextDigest || terminal.QueueItem != itemRef || terminal.AttemptID != attempt.AttemptID || terminal.GraphRunID != next.Item.GraphRunID || terminal.LoopExecutionID != child.LoopExecutionID || !reflect.DeepEqual(terminal.ArtifactIDs, []string{artifact.ID}) || !reflect.DeepEqual(terminal.ReceiptIDs, []string{receipt.ID}) || terminal.Authority != next.Claims[0].Authority || terminal.Authority.ID != artifact.AuthorityContextID || terminal.Authority.Digest != artifact.AuthorityContextDigest {
			t.Fatal("success evidence exact lineage mismatch")
		}
	}
	assertUnchanged(current)
	if err := s.Store.Load("queue-loop-reexecution-successor", next.Item.ItemID, &slot); !os.IsNotExist(err) {
		t.Fatal("denial reserved another successor", err)
	}
}
