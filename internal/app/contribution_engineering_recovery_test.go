package app

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"os"
	"testing"
)

const engineeringInspect = `{"read_files":["feature/value.go","feature/regression_test.go"]}`
const engineeringScope = `{"schema_version":"aegis.contribution.issue-engineering.v1","outcome":"Issue-specific repair","acceptance":"actual trusted behavior","writable_files":["feature/value.go","feature/regression_test.go"],"context_files":[],"exclusions":["do not expand"]}`

func TestEngineeringMalformedRecovery(t *testing.T) {
	for _, second := range []bool{false, true} {
		t.Run(map[bool]string{false: "first", true: "second"}[second], func(t *testing.T) {
			outputs := []string{`{"unknown":true}`, engineeringInspect, engineeringScope}
			if second {
				outputs = []string{engineeringInspect, `{"unknown":true}`, engineeringInspect, engineeringScope}
			}
			f := newEngineeringAppFixture(t, outputs)
			ctx := context.Background()
			key := "recover"
			if _, err := f.s.admitContributionAs(ctx, f.subject, "session", f.ref, f.reader.source.URL.URL, key, "local-only", f.reader); err == nil {
				t.Fatal("malformed output accepted")
			}
			before, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
			if err != nil {
				t.Fatal(err)
			}
			var intent contributionEngineeringIntent
			if err = f.s.Store.Load("contribution-engineering-intents", before.IntentID, &intent); err != nil {
				t.Fatal(err)
			}
			var dispatch execution.LaunchContract
			if err = f.s.Store.Load("contribution-engineering-dispatches", before.IntentID, &dispatch); err != nil {
				t.Fatal(err)
			}
			if dispatch.ParentDispatch.State != execution.StateFailed || !before.Quiescent || before.ReturnedTurns != before.ReservedTurns {
				t.Fatalf("untruthful failed dispatch/status: %+v %+v", dispatch.ParentDispatch, before)
			}
			sealed, err := f.s.admitContributionAs(ctx, f.subject, "fresh-session", f.ref, f.reader.source.URL.URL, key, "local-only", f.reader)
			if err != nil {
				t.Fatal("same-key recovery:", err)
			}
			after, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
			if err != nil {
				t.Fatal(err)
			}
			expected := uint32(3)
			if second {
				expected = 4
			}
			if after.RunID != before.RunID || !after.Deadline.Equal(before.Deadline) || sealed.RunID != before.RunID || after.ReservedTurns != expected || after.ReturnedTurns != expected {
				t.Fatalf("reset identity/budget: before=%+v after=%+v", before, after)
			}
			var retained contributionEngineeringIntent
			if err = f.s.Store.Load("contribution-engineering-intents", before.IntentID, &retained); err != nil || core.Digest(intent) != core.Digest(retained) {
				t.Fatal("intent mutated", err)
			}
			if f.reader.calls != 1 {
				t.Fatalf("reread issue: %d", f.reader.calls)
			}
			t.Logf("original run/deadline retained; reserved=returned=%d; failed dispatch preserved before recovery", expected)
		})
	}
}

func TestEngineeringPreparationControlsAndUnknown(t *testing.T) {
	f := newEngineeringAppFixture(t, []string{`{"unknown":true}`})
	ctx := context.Background()
	key := "controls"
	_, err := f.s.admitContributionAs(ctx, f.subject, "session", f.ref, f.reader.source.URL.URL, key, "local-only", f.reader)
	if err == nil {
		t.Fatal("expected malformed")
	}
	before, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.ControlContributionAs(ctx, f.subject, "fresh", key, false, "verified-protected-base-merge"); err == nil {
		t.Fatal("widening accepted")
	}
	after, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil || after.Digest != before.Digest {
		t.Fatal("denial wrote history", err)
	}
	if err = f.s.ControlContributionAs(ctx, f.subject, "fresh", key, false, "local-only"); err != nil {
		t.Fatal(err)
	}
	after, err = f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil || after.Control.Stop {
		t.Fatal("narrowing became stop", err)
	}
	if err = f.s.ControlContributionAs(ctx, f.subject, "fresh", key, true, ""); err != nil {
		t.Fatal(err)
	}
	if err = f.s.ControlContributionAs(ctx, f.subject, "fresh", key, false, "local-only"); err != nil {
		t.Fatal(err)
	}
	stopped, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil || !stopped.Control.Stop {
		t.Fatal("stop not sticky", err)
	}
	result, err := f.s.ResumeContributionAs(ctx, f.subject, key)
	if err != nil || result.Preparation == nil || !result.Preparation.Control.Stop {
		t.Fatal("supported stop readback", err)
	}
	turns, _ := os.ReadFile(f.counter)
	if string(turns) != "1" {
		t.Fatal("stopped inference replayed", string(turns))
	}
}

func TestEngineeringUnknownInterruptionNoReplay(t *testing.T) {
	f := newEngineeringAppFixture(t, []string{`{"unknown":true}`})
	ctx := context.Background()
	key := "unknown"
	_, _ = f.s.admitContributionAs(ctx, f.subject, "session", f.ref, f.reader.source.URL.URL, key, "local-only", f.reader)
	head, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil {
		t.Fatal(err)
	}
	// Crash boundary: an owned reservation exists, but no observed adapter return.
	next := head
	next.ReservedTurns++
	next.Quiescent = false
	next.Outcome = "interrupted_unknown"
	if _, err = f.s.appendEngineeringStatus(head, next); err != nil {
		t.Fatal(err)
	}
	result, err := f.s.ResumeContributionAs(ctx, f.subject, key)
	if err != nil || result.Preparation == nil || result.Preparation.Outcome != "interrupted_unknown" {
		t.Fatal("unknown readback", err)
	}
	if _, err = f.s.admitContributionAs(ctx, f.subject, "fresh", f.ref, f.reader.source.URL.URL, key, "local-only", f.reader); err == nil {
		t.Fatal("unknown replay admitted")
	}
	turns, _ := os.ReadFile(f.counter)
	if string(turns) != "1" {
		t.Fatal("unknown inference replayed", string(turns))
	}
	b, _ := json.Marshal(result.Preparation)
	t.Log(string(b))
}

func TestEngineeringSupportedResumePreparation(t *testing.T) {
	f := newEngineeringAppFixture(t, []string{`{"unknown":true}`, engineeringInspect, engineeringScope})
	ctx := context.Background()
	key := "supported-resume"
	_, err := f.s.admitContributionAs(ctx, f.subject, "session", f.ref, f.reader.source.URL.URL, key, "local-only", f.reader)
	if err == nil {
		t.Fatal("malformed accepted")
	}
	before, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil {
		t.Fatal(err)
	}
	// Execution is not host-admitted in this fixture. Resume must nevertheless
	// recover preparation and seal the same run, not strand it as fleet-not-found.
	_, executionErr := f.s.ResumeContributionAs(ctx, f.subject, key)
	c, _, err := f.s.ReadContributionAs(ctx, f.subject, key)
	if err != nil || c.RunID != before.RunID || !c.Deadline.Equal(before.Deadline) {
		t.Fatal("supported Resume did not recover preparation", err, executionErr)
	}
	after, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil || after.ReservedTurns != 3 || after.ReturnedTurns != 3 {
		t.Fatal("resume reset turns", err)
	}
	t.Logf("same-run supported preparation Resume verified; separate execution prerequisite=%v", executionErr)
}

func TestEngineeringStrictNarrowing(t *testing.T) {
	f := newEngineeringAppFixture(t, []string{`{"unknown":true}`, engineeringInspect, engineeringScope})
	ctx := context.Background()
	key := "strict-narrow"
	// Select the default merge profile, then explicitly narrow before Queue admission.
	p := *f.revision.Contribution
	p.Effect = "verified-protected-base-merge"
	revision, _, err := loop.NewContributionRevision("engineering-narrow", 1, "", f.template, p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.PublishLoopAs(ctx, f.subject, PublishLoopInput{AgentID: p.Controller.ID, Revision: revision, IdempotencyKey: "narrow-profile"})
	if err != nil {
		t.Fatal(err)
	}
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: revision.LoopID, Revision: 1, Digest: revision.Digest}
	_, err = f.s.SetLoopLifecycleAs(ctx, f.subject, ref.ID, SetLoopLifecycleInput{AgentID: p.Controller.ID, Loop: ref, State: loop.LifecycleActive, EventID: "narrow-active"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.admitContributionAs(ctx, f.subject, "session", ref, f.reader.source.URL.URL, key, "", f.reader)
	if err == nil {
		t.Fatal("malformed accepted")
	}
	if err = f.s.ControlContributionAs(ctx, f.subject, "fresh", key, false, "local-only"); err != nil {
		t.Fatal(err)
	}
	status, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil || status.Control.Stop || status.Control.Delivery != "local-only" {
		t.Fatal("invalid narrowing", err)
	}
	c, err := f.s.admitContributionAs(ctx, f.subject, "fresh", ref, f.reader.source.URL.URL, key, "", f.reader)
	if err != nil || c.Effect != "local-only" {
		t.Fatal("narrowed preparation did not seal", err)
	}
}
