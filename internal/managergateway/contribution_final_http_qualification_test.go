//go:build finalqualification

package managergateway

import (
	"github.com/berryhill/aegis/internal/delivery"
	"strings"
	"testing"
)

func init() {
	var fixtures finalFixtureRegistry
	delivery.FinalQualificationControllerGitHub = fixtures.resolve
	finalQualificationProtection = delivery.FinalQualificationProtection
	finalQualificationFactory = func(t *testing.T, p delivery.Plan) (delivery.Adapter, func() int) {
		remote, fixture := delivery.FinalQualificationGitHub(t, p)
		if strings.Contains(t.Name(), "CIRepair") || strings.Contains(t.Name(), "AdvancingBase") {
			cleanup, err := fixtures.register(p, func(plan delivery.Plan) (*delivery.GitHub, error) { fixture.SetPlan(plan); return remote, nil })
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
		}
		return remote, fixture.Count
	}
}
func TestCanonicalContributionFinalization(t *testing.T) {
	// Finish the listener contract check before the parallel roots can bind;
	// retain the same three required roots and the same claimed-port lifetime.
	t.Run("listener-isolation", delivery.FinalQualificationListenerRegression)
	t.Parallel()
	retainedDeliveryNativeComposition(t, true)
}
func TestCanonicalContributionLocalTargetFinalization(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}
func TestCanonicalContributionNoPublishTargetFinalization(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}
func TestCanonicalContributionNoMergeTargetFinalization(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}
func TestCanonicalContributionFailedChecksFinalization(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}
func TestCanonicalContributionContinuationFinalization(t *testing.T) {
	retainedDeliveryNativeComposition(t, true, true)
}

// Controlled native/real canonical store + loopback GitHub protocol, not live
// provider or actual protected remote qualification.
func TestCanonicalContributionConcurrentFinalization(t *testing.T) {
	retainedDeliveryNativeComposition(t, true, true)
}
func TestCanonicalContributionConcurrentWinnerFailure(t *testing.T) {
	retainedDeliveryNativeComposition(t, true, true)
}
func TestCanonicalContributionConcurrentWaiterCancellation(t *testing.T) {
	retainedDeliveryNativeComposition(t, true, true)
}

func TestContributionIssueIntentAdmission(t *testing.T) {
	retainedDeliveryNativeComposition(t, true)
}

func TestCanonicalContributionCIRepairFinalization(t *testing.T) {
	t.Parallel()
	retainedDeliveryNativeComposition(t, true)
}

// Actual canonical same-PR controller journey with a real nonconflicting Git
// upstream addition. HTTP and runtime are controlled; no live remote is claimed.
func TestCanonicalContributionAdvancingBaseFinalization(t *testing.T) {
	// Run alone before Go releases the other two paused roots: CPU contention
	// must not consume this journey's unchanged contribution deadline.
	retainedDeliveryNativeComposition(t, true)
}
