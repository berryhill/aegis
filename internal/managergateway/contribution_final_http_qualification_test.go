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
