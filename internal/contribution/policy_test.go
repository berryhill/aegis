package contribution

import "testing"

func TestContributionDeliveryClosedDefaultAndNarrowing(t *testing.T) {
	got, e := RestrictDelivery("", "")
	if e != nil || got != string(DeliveryMerge) {
		t.Fatalf("default %s %v", got, e)
	}
	for _, s := range []string{"no-merge", "no-publish", "local-only"} {
		if got, e := RestrictDelivery("", s); e != nil || got != s {
			t.Fatalf("restriction %s %v", got, e)
		}
	}
	for _, s := range []string{"verified-protected-base-merge", "no-merge", "arbitrary instructions"} {
		if _, e := RestrictDelivery("local-only", s); e == nil {
			t.Fatalf("accepted widening %s", s)
		}
	}
}
