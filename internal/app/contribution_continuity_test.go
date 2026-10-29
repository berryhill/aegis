package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
)

func TestContributionContinuityServiceDurabilityAndBudgets(t *testing.T) {
	ctx := context.Background()
	s, repo, subject, ref, reader := contributionFixture(t)
	now := s.Now()
	s.Now = func() time.Time { return now }
	c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "continuity", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	state := contribution.Continuity{}
	appendFact := func(kind, counter string) error {
		var e error
		state, e = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: kind, Counter: counter})
		return e
	}
	if err = appendFact("initialize", ""); err != nil {
		t.Fatal(err)
	}
	for i := uint8(0); i < c.CandidateBudget; i++ {
		if err = appendFact("reserve", "candidate"); err != nil {
			t.Fatal(err)
		}
	}
	sequence, digest := state.Sequence, state.Digest
	if err = appendFact("reserve", "candidate"); !errors.Is(err, contribution.ErrContinuityExhausted) {
		t.Fatalf("candidate reset %v", err)
	}
	_, state, _, err = s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	if state.Sequence != sequence || state.Digest != digest {
		t.Fatal("failed reservation mutated durable head")
	}
	if err = appendFact("reserve", "queue-attempt"); err != nil {
		t.Fatal(err)
	}
	if err = appendFact("reserve", "runtime-turn"); err != nil {
		t.Fatal(err)
	}
	if err = appendFact("reserve", "infrastructure-recheck"); err != nil {
		t.Fatal(err)
	}
	if err = appendFact("reserve", "decision-delivery"); err != nil {
		t.Fatal(err)
	}
	in := ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "checkpoint", Stage: "build", Candidate: "candidate-one", Evidence: reference.DigestRef{SchemaVersion: reference.DigestRefSchemaVersion, ID: "build-evidence", Digest: contribution.Digest([]byte("observed-build"))}}
	state, err = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, in)
	if err != nil {
		t.Fatal(err)
	}
	// Replaying a stale response cannot reserve or append twice.
	if _, err = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, in); !errors.Is(err, contribution.ErrContinuityConflict) {
		t.Fatal(err)
	}
	if err = repo.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := fleetbadger.Open(ctx, filepath.Join(s.Config.StateDir, "persistence", "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	configureLocalImportFleet(t, s, reopened)
	original, state2, events, err := s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if err != nil {
		t.Fatal(err)
	}
	if original.Digest != c.Digest || state2.Digest != state.Digest || state2.Used != state.Used || state2.Stage != "build" || len(events) != int(state.Sequence) {
		t.Fatalf("restart lost lineage %+v", state2)
	}
	// Fresh authentication observes but cannot silently continue the original role.
	in.ExpectedSequence = state2.Sequence
	in.ExpectedDigest = state2.Digest
	if _, err = s.RecordContributionContinuityAs(ctx, subject, "new-session", c.Key, in); !errors.Is(err, ErrDenied) {
		t.Fatalf("silent continuation %v", err)
	}
	foreign := subject
	foreign.PrincipalID = "foreign"
	if _, _, _, err = s.ObserveContributionContinuityAs(ctx, foreign, c.Key); err == nil {
		t.Fatal("foreign observation")
	}
	now = c.Deadline
	// A newly authenticated transport subject can observe/cancel, but neither
	// authentication nor a retained original session label moves the deadline.
	subject.AuthenticatedAt = now
	subject.ExpiresAt = now.Add(time.Minute)
	if _, err = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, in); !errors.Is(err, contribution.ErrContinuityExhausted) {
		t.Fatalf("deadline renewed %v", err)
	}
	// Cancellation survives expiry and fences the actual existing execution control.
	in.Kind = "cancel-requested"
	in.Stage = ""
	in.Candidate = ""
	in.Evidence = reference.DigestRef{}
	state, err = s.RecordContributionContinuityAs(ctx, subject, "fresh-cancellation-session", c.Key, in)
	if err != nil {
		t.Fatal(err)
	}
	control, err := reopened.GetContributionControl(ctx, c.QueueItemID)
	if err != nil || !control.Stop || !state.CancelRequested {
		t.Fatalf("cancel did not fence existing owner: %+v %v", control, err)
	}
	item, err := reopened.GetQueueItem(ctx, c.QueueItemID)
	if err != nil || item.GraphRunID != c.RunID {
		t.Fatal("cancellation changed identity")
	}
	if _, err = reopened.GetQueueRuntimeBinding(ctx, c.QueueItemID); !errors.Is(err, fleet.ErrNotFound) {
		t.Fatalf("manufactured runtime binding %v", err)
	}
}

func TestContributionContinuityDecisionsAreNotApproval(t *testing.T) {
	ctx := context.Background()
	s, repo, subject, ref, reader := contributionFixture(t)
	c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "decisions", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	state := contribution.Continuity{}
	record := func(kind, counter, id, outcome string) error {
		next, e := s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: kind, Counter: counter, DecisionID: id, DecisionOutcome: outcome})
		if e == nil {
			state = next
		}
		return e
	}
	if err = record("initialize", "", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []string{"unanswered", "denied", "expired", "dialog-cancelled", "unavailable", "timeout", "unknown"} {
		if err = record("reserve", "decision-delivery", "", ""); err != nil {
			t.Fatal(err)
		}
		if err = record("decision", "", outcome, "requested"); err != nil {
			t.Fatal(err)
		}
		if outcome != "unavailable" {
			if err = record("decision", "", outcome, "delivered"); err != nil {
				t.Fatal(err)
			}
		}
		if err = record("decision", "", outcome, outcome); err != nil {
			t.Fatalf("%s %v", outcome, err)
		}
	}
	if err = record("decision", "", "unknown", "approved"); err == nil {
		t.Fatal("caller minted approval")
	}
	if err = record("decision", "", "unavailable", "delivered"); err == nil {
		t.Fatal("unchanged unavailable work redispatched")
	}
	if len(state.Decisions) != 7 {
		t.Fatal(state.Decisions)
	}
	// Already-cancelled HTTP does not become whole-run cancellation or partial write.
	aborted, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.RecordContributionContinuityAs(aborted, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "cancel-requested"})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	control, err := repo.GetContributionControl(ctx, c.QueueItemID)
	if err != nil || control.Stop {
		t.Fatalf("HTTP abort cancelled run %v", err)
	}
	got, _, err := repo.GetContributionContinuity(ctx, c.QueueItemID)
	if err != nil || got.Digest != state.Digest {
		t.Fatal("aborted request wrote state")
	}
}

func TestContributionContinuityRejectsRevokedRoleButRetainsObservation(t *testing.T) {
	ctx := context.Background()
	s, repo, subject, ref, reader := contributionFixture(t)
	c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "disabled-role", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "initialize"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := repo.LatestAgentRevision(ctx, c.Builder.ID)
	if err != nil {
		t.Fatal(err)
	}
	agent.Revision++
	agent.Lifecycle = registry.LifecycleDisabled
	agent, err = registry.SealRevision(agent)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.PublishAgentRevision(ctx, agent, fleet.AuditFact{Event: core.AuditEvent{Type: "agent.revised", SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Outcome: "recorded", Reason: "fixture-disable"}}); err != nil {
		t.Fatal(err)
	}
	in := ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "reserve", Counter: "runtime-turn"}
	if _, err = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, in); !errors.Is(err, ErrDenied) {
		t.Fatalf("disabled role admitted %v", err)
	}
	_, retained, _, err := s.ObserveContributionContinuityAs(ctx, subject, c.Key)
	if err != nil || retained.Digest != state.Digest {
		t.Fatal("observation lost after role disable")
	}
	in.Kind = "cancel-requested"
	in.Counter = ""
	if _, err = s.RecordContributionContinuityAs(ctx, subject, "fresh-owner", c.Key, in); err != nil {
		t.Fatalf("disabled role prevented owner cancellation %v", err)
	}
}

func TestContributionContinuityCancelBeforeInitialize(t *testing.T) {
	ctx := context.Background()
	s, repo, subject, ref, reader := contributionFixture(t)
	c, err := s.admitContributionAs(ctx, subject, "original", ref, reader.source.URL.URL, "cancel-first", "local-only", reader)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.RecordContributionContinuityAs(ctx, subject, "new-owner-session", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, Kind: "cancel-requested"})
	if err != nil || !state.CancelRequested {
		t.Fatalf("%+v %v", state, err)
	}
	control, err := repo.GetContributionControl(ctx, c.QueueItemID)
	if err != nil || !control.Stop {
		t.Fatal("missing actual control fence")
	}
	if _, err = s.RecordContributionContinuityAs(ctx, subject, "original", c.Key, ContributionContinuityInput{ContractDigest: c.Digest, ExpectedSequence: state.Sequence, ExpectedDigest: state.Digest, Kind: "initialize"}); !errors.Is(err, contribution.ErrContinuityFenced) {
		t.Fatalf("cancel revived %v", err)
	}
}
