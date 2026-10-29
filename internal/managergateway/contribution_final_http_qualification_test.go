//go:build finalqualification

package managergateway

import (
	"github.com/berryhill/aegis/internal/delivery"
	"testing"
)

func init() {
	finalQualificationProtection = delivery.FinalQualificationProtection
	finalQualificationFactory = func(t *testing.T, p delivery.Plan) (delivery.Adapter, func() int) {
		remote, fixture := delivery.FinalQualificationGitHub(t, p)
		return remote, fixture.Count
	}
}
func TestCanonicalContributionFinalization(t *testing.T) { retainedDeliveryNativeComposition(t, true) }
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
