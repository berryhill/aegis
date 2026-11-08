package app

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"os"
	"path/filepath"
	"testing"
)

func TestEngineeringMissingProvisioningRetainedDecision(t *testing.T) {
	f := newEngineeringAppFixture(t, nil)
	ctx := context.Background()
	key := "missing-provision"
	// Remove only the disposable test fixture's synthetic ready receipt. No
	// production authority or approved decision is constructed by this test.
	if err := os.Remove(filepath.Join(f.s.Store.Root(), "receipts", "ready-contribution-builder.json")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.s.ContributionDecisionDelivery = func(ctx context.Context, sub core.Subject, key, role string) (string, error) {
		return f.s.DeliverContributionDecisionAs(ctx, sub, key, role, func(context.Context, ContributionDecisionInput) string { calls++; return "approved" })
	}
	_, err := f.s.admitContributionAs(ctx, f.subject, "session", f.ref, f.reader.source.URL.URL, key, "local-only", f.reader)
	if err == nil {
		t.Fatal("companion claim manufactured approval")
	}
	before, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil {
		t.Fatal(err)
	}
	if before.Decision == nil || before.Decision.Status != "unknown" || before.Decision.ApprovalID != "" || before.Decision.ReceiptID != "" || before.DecisionDeliveries != 1 || before.ReservedTurns != 0 {
		t.Fatalf("missing retained unknown decision: %+v", before)
	}
	retained, err := f.s.ReadContributionDecisionAs(ctx, f.subject, key, before.Decision.ID)
	if err != nil || retained != *before.Decision {
		t.Fatal("supported decision readback", err)
	}
	result, err := f.s.ResumeContributionAs(ctx, f.subject, key)
	if err == nil || result.Preparation == nil {
		t.Fatal("missing prerequisite misrepresented", err)
	}
	after, err := f.s.ReadContributionEngineeringAs(ctx, f.subject, key)
	if err != nil || after.DecisionDeliveries != 1 || calls != 1 || after.RunID != before.RunID || after.ReservedTurns != 0 {
		t.Fatal("decision redelivered or scope reset", err)
	}
	verified, err := f.s.hasVerifiedReceipt(before.Decision.Binding.Charter.Digest)
	if err != nil || verified {
		t.Fatal("approval/receipt manufactured", err)
	}
	if _, err = os.Stat(f.counter); !os.IsNotExist(err) {
		t.Fatal("planner launched before provisioning", err)
	}
	if before.Decision.Role != "builder" || before.Decision.Stage != "engineering-preparation" || !contribution.ValidKey(before.Decision.ID) {
		t.Fatal("decision not internally bound")
	}
}
