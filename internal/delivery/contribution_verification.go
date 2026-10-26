package delivery

// ValidateIntentForPlan checks the exact existing canonical intent builder. The
// verifier must derive plan from independently reloaded accepted source and
// reviewed controller policy, never from the observed intent itself. This pure
// comparison grants no authority, reserves no effect and performs no I/O.
func ValidateIntentForPlan(plan Plan, observed Intent) error {
	expected, err := intentFor(plan, observed.Operation)
	if err != nil || expected != observed {
		return ErrDenied
	}
	return nil
}
