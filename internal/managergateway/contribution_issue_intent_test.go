package managergateway

import (
	"context"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/delivery"
)

// Controlled protocol qualification, not production credential/resource scope.
// This wraps the existing real frame/preclosure/finalizer instead of accepting
// records or constructing an expected success report.
type issueIntentFrameCompletion struct {
	*managerFrameCompletion
	t                                            *testing.T
	beforeReserve, unknown, confirmed, negatives bool
}

func (f *issueIntentFrameCompletion) Admit(ctx context.Context, intent delivery.Intent, writing bool) error {
	if err := (assessmentFixtureAuthority{}).Admit(ctx, intent, writing); err != nil {
		return err
	}
	if intent.Operation != delivery.ReconcileIssue {
		return nil
	}
	actual, err := f.get(ctx)
	if err != nil {
		return err
	}
	if err = actual.VerifyContributionIssueIntent(ctx, intent); err != nil {
		return err
	}
	history, err := f.s.ReadContributionDeliveryHistoryAs(ctx, f.subject, f.key)
	if err != nil {
		return err
	}
	found := false
	for _, chain := range history.Histories {
		last := chain[len(chain)-1].Record
		if last.Intent.Operation == delivery.ReconcileIssue {
			found = true
			if last.Intent != intent {
				f.t.Fatal("substituted issue reservation")
			}
			if last.Outcome == delivery.Unknown {
				f.unknown = true
			}
		}
	}
	if !found {
		f.beforeReserve = true
	}
	if !f.negatives {
		f.negatives = true
		for name, mutate := range map[string]func(*delivery.Intent){
			"candidate": func(i *delivery.Intent) { i.Candidate = "sha256:" + strings.Repeat("1", 64) },
			"run":       func(i *delivery.Intent) { i.Run += "-foreign" },
			"operation": func(i *delivery.Intent) { i.Operation = delivery.Merge },
			"role":      func(i *delivery.Intent) { i.RoleBinding = "sha256:" + strings.Repeat("2", 64) },
			"pr":        func(i *delivery.Intent) { i.PR++ },
			"payload":   func(i *delivery.Intent) { i.Payload = "sha256:" + strings.Repeat("3", 64) },
		} {
			wrong := intent
			mutate(&wrong)
			if err := actual.VerifyContributionIssueIntent(ctx, wrong); err == nil {
				f.t.Fatal("substituted issue intent accepted", name)
			}
		}
		if err := actual.VerifyContributionIssueIntent(ctx, intent); err != nil {
			f.t.Fatal("negative probes altered valid control", err)
		}
	}
	return nil
}

func (f *issueIntentFrameCompletion) FinalizeContributionCompletion(ctx context.Context, ref delivery.Reference) error {
	history, err := f.s.ReadContributionDeliveryHistoryAs(ctx, f.subject, f.key)
	if err != nil {
		return err
	}
	actual, err := f.get(ctx)
	if err != nil {
		return err
	}
	for _, chain := range history.Histories {
		last := chain[len(chain)-1].Record
		if last.Intent.Operation != delivery.ReconcileIssue {
			continue
		}
		if last.Outcome != delivery.Confirmed {
			f.t.Fatal("issue readback not confirmed")
		}
		if err := actual.VerifyContributionIssueIntent(ctx, last.Intent); err != nil {
			return err
		}
		f.confirmed = true
	}
	if !f.beforeReserve || !f.unknown || !f.confirmed || !f.negatives {
		f.t.Fatalf("issue admission boundaries incomplete: before=%t unknown=%t confirmed=%t negatives=%t", f.beforeReserve, f.unknown, f.confirmed, f.negatives)
	}
	return actual.FinalizeContributionCompletion(ctx, ref)
}
