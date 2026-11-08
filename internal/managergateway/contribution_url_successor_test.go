package managergateway

import (
	"context"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"testing"
)

func TestContributionURLSuccessorManagerUsesRetainedDecision(t *testing.T) {
	ctx := context.Background()
	a, repo, subject, oldRef, reader := contributionFixture(t, true)
	old, e := a.GetLoopViewAs(ctx, subject, oldRef.ID, oldRef.Revision)
	if e != nil {
		t.Fatal(e)
	}
	p := *old.Revision.Contribution
	managed := *p.Managed
	managed.Kind = loop.EngineeringExecutionKind
	managed.ReviewMode = "issue-journeys.v1"
	for i := range managed.ReviewAssets {
		managed.ReviewAssets[i].ApprovedDigest = ""
		managed.ReviewAssets[i].CriterionID = managed.ReviewAssets[i].Scope
	}
	p.Managed = &managed
	p.FinalVerification = "aegis.contribution.final.v1"
	p.Engineering = &loop.IssueEngineeringPolicy{WritablePrefixes: []string{"feature"}, MaxWritableFiles: 4}
	var template loop.VerifiedImplementation
	for _, step := range old.Revision.Steps {
		if step.Implementation != nil {
			template = *step.Implementation
		}
	}
	next, _, e := loop.NewContributionRevision(oldRef.ID, 2, oldRef.Digest, template, p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: p.Controller.ID, Revision: next, ExpectedPreviousDigest: oldRef.Digest, IdempotencyKey: "url-successor"}); e != nil {
		t.Fatal(e)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: next.LoopID, Revision: next.Revision, Digest: next.Digest}
	if len(old.History) == 0 {
		t.Fatal("missing predecessor lifecycle history")
	}
	if _, e = a.SetLoopLifecycleAs(ctx, subject, ref.ID, app.SetLoopLifecycleInput{AgentID: p.Controller.ID, Loop: ref, State: loop.LifecycleActive, EventID: "url-active", ExpectedPreviousDigest: old.History[len(old.History)-1].Digest}); e != nil {
		t.Fatal(e)
	}
	a.ContributionReader = reader
	calls := 0
	a.ContributionDecisionDelivery = func(_ context.Context, sub core.Subject, key, role string) (string, error) {
		calls++
		if sub.ID != subject.ID || role != "builder" || key != app.ContributionIntentKey(ref, reader.source.URL.URL, "") {
			t.Fatal("wrong independent decision binding")
		}
		return "unavailable", nil
	}
	gateway, e := New(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	opened, e := gateway.Open(ctx, subject)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = gateway.Execute(ctx, subject, opened.ID, opened.Token, "/contribute select "+ref.ID); e != nil {
		t.Fatal(e)
	}
	// URL-only explicit invocation reaches the actual Engineering prerequisite
	// journey, without any caller-supplied workspace, session or receipt binding.
	if _, e = gateway.Execute(ctx, subject, opened.ID, opened.Token, "/contribute run "+reader.source.URL.URL); e == nil {
		t.Fatal("missing independent provisioning became success")
	}
	if calls != 1 {
		t.Fatal("independent decision was not delivered", calls)
	}
	key := app.ContributionIntentKey(ref, reader.source.URL.URL, "")
	status, e := a.ReadContributionEngineeringAs(ctx, subject, key)
	if e != nil || status.ReservedTurns != 0 {
		t.Fatal("lost/spent held intent", e)
	}
	items, e := repo.ListQueueItems(ctx)
	if e != nil || len(items) != 0 {
		t.Fatal("prerequisite bypassed", e, len(items))
	}
	previous, e := repo.GetLoopRevision(ctx, oldRef.ID, oldRef.Revision)
	if e != nil || previous.Digest != oldRef.Digest || len(previous.Inputs) != 0 {
		t.Fatal("history rewritten", e)
	}
}
