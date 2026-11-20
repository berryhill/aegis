package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/store"
)

func loopEditHuman(s *Service, sub core.Subject) core.Subject {
	human := core.Subject{ID: "password:independent-history-fixture", Kind: "principal", PrincipalID: sub.PrincipalID, Issuer: "aegis-principal-auth", Method: "password", AuthenticatedAt: s.Now(), ExpiresAt: sub.ExpiresAt}
	s.DashboardActionAuthentication = func(got core.Subject) error {
		if !reflect.DeepEqual(got, human) {
			return ErrDenied
		}
		return nil
	}
	return human
}

func loopEditStatus(t *testing.T, e LoopModelEdit) string {
	t.Helper()
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Status      string         `json:"status"`
		Publication *PublishedLoop `json:"publication"`
	}
	if err = json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if view.Status == "published" && (view.Publication == nil || view.Publication.PendingEdit != nil || !view.Publication.Decision.Idempotent || view.Publication.Revision.Digest != e.Input.Revision.Digest) {
		t.Fatal("missing exact publication readback")
	}
	return view.Status
}

func TestOwnedLoopModelEditHistoricalPublication(t *testing.T) {
	s, sub, input, old := loopModelEditFixture(t)
	ctx := context.Background()
	selection := OwnedLoopModelUpdateInput{AgentID: input.AgentID, Provider: "openai-codex", Model: "gpt-6.2-sol"}
	pending, err := s.UpdateOwnedLoopModelAs(ctx, sub, old.Revision.LoopID, 1, selection)
	if err != nil || pending.PendingEdit == nil {
		t.Fatalf("pending: %v", err)
	}
	edit := *pending.PendingEdit
	human := loopEditHuman(s, sub)
	if _, err = s.ConfirmOwnedLoopModelAs(ctx, human, edit); err != nil {
		t.Fatal(err)
	}
	pub, err := s.PublishOwnedLoopEditAs(ctx, sub, edit.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.GetLoopViewAs(ctx, sub, pub.Revision.LoopID, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"published", "later-agent", "expired"} {
		t.Run(stage, func(t *testing.T) {
			if stage == "later-agent" {
				agent, e := s.GetFleetAgentAs(ctx, sub, input.AgentID, 0)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = s.SetAgentLifecycleAs(ctx, sub, input.AgentID, SetAgentLifecycleInput{Expected: agentRevisionRef(agent.Revision), Lifecycle: registry.LifecycleDisabled}); e != nil {
					t.Fatal(e)
				}
			}
			if stage == "expired" {
				now := edit.ExpiresAt
				s.Now = func() time.Time { return now }
				sub.AuthenticatedAt, sub.ExpiresAt = now, now.Add(time.Minute)
				sub = loopEditHuman(s, sub) // new independent session, same principal
			}
			auditBefore, e := s.FleetRepository.AuditEvents(ctx)
			if e != nil {
				t.Fatal(e)
			}
			got, e := s.UpdateOwnedLoopModelAs(ctx, sub, old.Revision.LoopID, 1, selection)
			if e != nil || got.PendingEdit != nil || !got.Decision.Idempotent || !reflect.DeepEqual(got.Revision, pub.Revision) {
				t.Errorf("published update regressed to pending/error: pending=%v err=%v", got.PendingEdit != nil, e)
			}
			read, e := s.ReadOwnedLoopEditAs(ctx, sub, edit.ID)
			if e != nil || loopEditStatus(t, read) != "published" {
				t.Errorf("historical read status: %s %v", loopEditStatus(t, read), e)
			}
			got, e = s.PublishOwnedLoopEditAs(ctx, sub, edit.ID)
			if e != nil || !got.Decision.Idempotent || !reflect.DeepEqual(got.Revision, pub.Revision) {
				t.Errorf("historical publish replay: %v", e)
			}
			after, e := s.GetLoopViewAs(ctx, sub, pub.Revision.LoopID, 2)
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("publication/provenance/lifecycle changed")
			}
			auditAfter, e := s.FleetRepository.AuditEvents(ctx)
			if e != nil || !reflect.DeepEqual(auditBefore, auditAfter) {
				t.Fatal("historical reconciliation performed a new effect")
			}
		})
	}
	if _, err = s.ConfirmOwnedLoopModelAs(ctx, loopEditHuman(s, sub), edit); err == nil {
		t.Fatal("old review regained authority")
	}
}

func TestOwnedLoopModelEditHistoryDoesNotConsumePendingCapacity(t *testing.T) {
	s, sub, input, _ := loopModelEditFixture(t)
	ctx := context.Background()
	var retained []LoopModelEdit
	for i := 0; i < DoerDraftMaxRetained; i++ {
		input.IdempotencyKey = fmt.Sprintf("pending-%d", i)
		got, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, *got.PendingEdit)
	}
	input.IdempotencyKey = "over-capacity"
	if _, err := s.SubmitOwnedLoopEditAs(ctx, sub, input); !errors.Is(err, ErrDoerDraftCapacity) {
		t.Fatalf("active bound: %v", err)
	}
	now := retained[0].ExpiresAt
	s.Now = func() time.Time { return now }
	sub.AuthenticatedAt, sub.ExpiresAt = now, now.Add(time.Minute)
	input.IdempotencyKey = "after-history"
	got, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
	if err != nil || got.PendingEdit == nil {
		t.Fatalf("historical edits permanently exhaust capacity: %v", err)
	}
	reopened, err := store.Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	s.Store = reopened
	for _, old := range retained {
		read, err := s.ReadOwnedLoopEditAs(ctx, sub, old.ID)
		if err != nil || loopEditStatus(t, read) != "expired" || !reflect.DeepEqual(read.Input, old.Input) || read.ExpiresAt != old.ExpiresAt {
			t.Fatalf("lost/renewed history: %v", err)
		}
		if _, err = s.PublishOwnedLoopEditAs(ctx, sub, old.ID); !errors.Is(err, ErrExpired) {
			t.Fatalf("expired permission reused: %v", err)
		}
		if _, err = s.ConfirmOwnedLoopModelAs(ctx, loopEditHuman(s, sub), old); err == nil {
			t.Fatal("expired decision approved")
		}
		if _, err = s.SubmitOwnedLoopEditAs(ctx, sub, old.Input); !errors.Is(err, ErrExpired) {
			t.Fatalf("same key renewed: %v", err)
		}
	}
	var raw json.RawMessage
	if err = s.Store.Load(loopModelEditKind, doerDraftSlot(sub.PrincipalID), &raw); err != nil {
		t.Fatal(err)
	}
	set, err := decodeLoopModelEdits(raw, sub.PrincipalID)
	if err != nil || len(set.Edits) != 1 {
		t.Fatalf("active blob still contains history: count=%d err=%v", len(set.Edits), err)
	}
}

func TestOwnedLoopModelEditConcurrentPendingBound(t *testing.T) {
	s, sub, input, _ := loopModelEditFixture(t)
	var wg sync.WaitGroup
	results := make(chan error, DoerDraftMaxRetained+4)
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := input
			in.IdempotencyKey = fmt.Sprintf("concurrent-%d", i)
			_, err := s.SubmitOwnedLoopEditAs(context.Background(), sub, in)
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, ErrDoerDraftCapacity) {
			t.Fatal(err)
		}
	}
	if accepted != DoerDraftMaxRetained {
		t.Fatalf("pending cap accepted %d", accepted)
	}
}

func TestOwnedLoopModelEditTerminalHistoryAndApprovedFastPath(t *testing.T) {
	s, sub, input, old := loopModelEditFixture(t)
	ctx := context.Background()
	var retained []LoopModelEdit
	for i := 0; i < DoerDraftMaxRetained; i++ {
		input.IdempotencyKey = fmt.Sprintf("terminal-%d", i)
		result, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, *result.PendingEdit)
	}
	human := loopEditHuman(s, sub)
	if _, err := s.ConfirmOwnedLoopModelAs(ctx, human, retained[0]); err != nil {
		t.Fatal(err)
	}
	// Archive-only simulates interruption before bounded-index compaction.
	if err := s.archiveLoopModelEdits(ctx, sub); err != nil {
		t.Fatal(err)
	}
	for _, edit := range retained {
		read, err := s.ReadOwnedLoopEditAs(ctx, sub, edit.ID)
		if err != nil || read.Status != "approved" {
			t.Fatalf("approved outcome lost: %v", err)
		}
		if _, err = s.ConfirmOwnedLoopModelAs(ctx, human, edit); err == nil {
			t.Fatal("old unfinished decision acquired authority")
		}
	}
	published, err := s.PublishOwnedLoopEditAs(ctx, sub, retained[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	// The already-authorized EXACT requested lane needs no additional review.
	fast, err := s.UpdateOwnedLoopModelAs(ctx, sub, old.Revision.LoopID, 2, OwnedLoopModelUpdateInput{AgentID: input.AgentID, Provider: "openai-codex", Model: "gpt-6.2-sol"})
	if err != nil || fast.PendingEdit != nil || fast.Revision.Revision != 3 {
		t.Fatalf("authorized fast path: %v", err)
	}
	for _, step := range fast.Revision.Steps {
		if step.Executable != nil && step.Executable.Backend == "hermes" {
			if p, m := step.EffectiveLane(); p != "openai-codex" || m != "gpt-6.2-sol" {
				t.Fatal("wrong requested lane")
			}
		}
	}
	// Independent fixture approval removes that lane on a later exact Agent;
	// no expired/stale review can re-add it, but a new pending request may exist.
	charter := retained[0].Original.Charter
	charter.Revision = retained[0].Proposed.Charter.Revision + 1
	canonical, err := core.Canonicalize(charter)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetFleetAgentAs(ctx, sub, input.AgentID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApproveAgentCharterAs(ctx, sub, input.AgentID, ApproveAgentCharterInput{Expected: agentRevisionRef(current.Revision), Charter: RevisionReference(input.AgentID, charter.Revision, canonical.Digest)}); err != nil {
		t.Fatal(err)
	}
	next, err := s.UpdateOwnedLoopModelAs(ctx, sub, old.Revision.LoopID, 3, OwnedLoopModelUpdateInput{AgentID: input.AgentID, Provider: "openai-codex", Model: "gpt-6.2-sol"})
	if err != nil || next.PendingEdit == nil {
		t.Fatalf("terminal history consumed capacity: %v", err)
	}
	reopened, err := store.Open(s.Store.Root())
	if err != nil {
		t.Fatal(err)
	}
	s.Store = reopened
	for i, edit := range retained {
		read, err := s.ReadOwnedLoopEditAs(ctx, sub, edit.ID)
		want := "stale"
		if i == 0 {
			want = "published"
		}
		if err != nil || read.Status != want || !reflect.DeepEqual(read.Input, edit.Input) {
			t.Fatalf("terminal history %d: status=%s err=%v", i, read.Status, err)
		}
	}
	replay, err := s.PublishOwnedLoopEditAs(ctx, sub, retained[0].ID)
	if err != nil || !replay.Decision.Idempotent || replay.Revision.Digest != published.Revision.Digest || replay.Provenance == nil {
		t.Fatalf("archived published outcome: %v", err)
	}
	// Also exercise the persisted request verifier directly against changed key,
	// predecessor, validation and provenance, without changing historical facts.
	request := loop.PublishRequest{Revision: replay.Revision, Validation: replay.Validation, Provenance: *replay.Provenance, ExpectedPreviousDigest: retained[0].Input.ExpectedPreviousDigest, IdempotencyKey: retained[0].Input.IdempotencyKey}
	if err = s.FleetRepository.VerifyLoopPublication(ctx, request); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"key", "predecessor", "validation", "provenance"} {
		changed := request
		switch field {
		case "key":
			changed.IdempotencyKey = "foreign-key"
		case "predecessor":
			changed.ExpectedPreviousDigest = ""
		case "validation":
			changed.Validation.Revision++
		case "provenance":
			changed.Provenance.OwnerID = "foreign-owner"
		}
		if err = s.FleetRepository.VerifyLoopPublication(ctx, changed); err == nil {
			t.Fatalf("changed %s accepted", field)
		}
	}
}

func TestOwnedLoopModelEditDoesNotReplayDifferentPublicationKey(t *testing.T) {
	s, sub, input, _ := loopModelEditFixture(t)
	ctx := context.Background()
	pending, err := s.SubmitOwnedLoopEditAs(ctx, sub, input)
	if err != nil {
		t.Fatal(err)
	}
	edit := *pending.PendingEdit
	if _, err = s.ConfirmOwnedLoopModelAs(ctx, loopEditHuman(s, sub), edit); err != nil {
		t.Fatal(err)
	}
	different := input
	different.IdempotencyKey = "separate-publication"
	if _, err = s.PublishLoopAs(ctx, sub, different); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishOwnedLoopEditAs(ctx, sub, edit.ID); err == nil {
		t.Fatal("equal digest with different key accepted")
	}
	if _, err = s.SubmitOwnedLoopEditAs(ctx, sub, input); err == nil {
		t.Fatal("equal digest with different key reported pending/published")
	}
}
