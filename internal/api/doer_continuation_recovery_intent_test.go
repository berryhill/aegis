package api

import (
	"context"
	"net/url"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
)

func TestDoerContinuationRecoveryExactOriginalIntent(t *testing.T) {
	mutations := map[string]func(*app.DoerContinuationIntent){
		"requester": func(i *app.DoerContinuationIntent) {
			i.Requester.AuthenticatedAt = i.Requester.AuthenticatedAt.Add(time.Second)
		},
		"config":      func(i *app.DoerContinuationIntent) { i.ConfigIdentity = "changed" },
		"deployment":  func(i *app.DoerContinuationIntent) { i.DeploymentID = "changed" },
		"agent":       func(i *app.DoerContinuationIntent) { i.Agent.Revision++ },
		"loop":        func(i *app.DoerContinuationIntent) { i.Loop.Revision++ },
		"draft":       func(i *app.DoerContinuationIntent) { i.DraftVersion++ },
		"publication": func(i *app.DoerContinuationIntent) { i.PublicationKey = "changed" },
		"expiry":      func(i *app.DoerContinuationIntent) { i.ExpiresAt = i.ExpiresAt.Add(-time.Second) },
		"run":         func(i *app.DoerContinuationIntent) { i.Run.Activate = false },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			svc, executor, intent := continuationFixture(t)
			_, err := svc.ApproveDoerContinuationAs(context.Background(), executor, intent)
			if err != nil {
				t.Fatal(err)
			}
			before := continuationState(t, svc)
			changed := intent
			mutate(&changed)
			id, resolved, err := svc.RecoverDoerContinuationAs(context.Background(), intent.Requester, intent.SessionID, changed)
			if err == nil || id != "" || !reflect.DeepEqual(resolved, core.Subject{}) {
				t.Fatal("changed original intent recovered authority")
			}
			if !reflect.DeepEqual(before, continuationState(t, svc)) {
				t.Fatal("mismatch changed custody")
			}
		})
	}
}

func TestDoerContinuationRecoveryMissingAndExpired(t *testing.T) {
	svc, executor, intent := continuationFixture(t)
	before := continuationState(t, svc)
	id, resolved, err := svc.RecoverDoerContinuationAs(context.Background(), intent.Requester, intent.SessionID, intent)
	if err != nil || id != "" || !reflect.DeepEqual(resolved, core.Subject{}) {
		t.Fatal("missing record not distinguished", err)
	}
	if !reflect.DeepEqual(before, continuationState(t, svc)) {
		t.Fatal("missing read initialized custody")
	}
	id, err = svc.ApproveDoerContinuationAs(context.Background(), executor, intent)
	if err != nil {
		t.Fatal(err)
	}
	before = continuationState(t, svc)
	svc.Now = func() time.Time { return intent.ExpiresAt }
	recovered, resolved, err := svc.RecoverDoerContinuationAs(context.Background(), intent.Requester, intent.SessionID, intent)
	if err == nil || recovered != "" || !reflect.DeepEqual(resolved, core.Subject{}) {
		t.Fatal("expired original recovered")
	}
	signed, readErr := svc.Store.ReadDoerContinuation(id)
	if readErr != nil || !signed.Executor.AuthenticatedAt.Equal(executor.AuthenticatedAt) || !signed.Intent.ExpiresAt.Equal(intent.ExpiresAt) {
		t.Fatal("expired recovery refreshed time")
	}
	if !reflect.DeepEqual(before, continuationState(t, svc)) {
		t.Fatal("expired recovery changed custody")
	}
}

func TestDoerPortalRecoveryLostCompanionResponse(t *testing.T) {
	p, e, cookie, csrf, d, subject, session := portalFixture(t)
	v, err := p.retain(context.Background(), subject, session, d.ID, "lost-response-run")
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := strconv.ParseUint(p.svc.Config.Principal.UID, 10, 32)
	executor, err := p.svc.AuthenticateUnixPeer(context.Background(), uint32(uid))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	id := ""
	p.launch = func(ctx context.Context, _ string, _ func(int)) string {
		calls++
		id, err = p.svc.ApproveDoerContinuationAs(ctx, executor, v.Review.Intent)
		if err != nil {
			t.Fatal(err)
		}
		// No assignment to portal.ApprovalID: response disappeared after signed save.
		return "unknown"
	}
	for j := 0; j < 2; j++ {
		out := portalPost(e, cookie, csrf, url.Values{"action": {"request-local-review"}, "intent_id": {v.ID}})
		if out.Code != 200 {
			t.Fatalf("lost response recovery %d %s", out.Code, out.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("lost response relaunched: %d", calls)
	}
	saved, err := p.reload(context.Background(), v.ID)
	if err != nil || saved.ApprovalID != id || saved.Status != "approved" {
		t.Fatal("lost response not recovered", err)
	}
	signed, err := p.svc.Store.ReadDoerContinuation(id)
	if err != nil || !reflect.DeepEqual(signed.Executor, executor) || !signed.Intent.ExpiresAt.Equal(v.Review.Intent.ExpiresAt) {
		t.Fatal("lost response replaced original executor or expiry", err)
	}
}
