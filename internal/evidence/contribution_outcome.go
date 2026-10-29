package evidence

// Contribution distinctions map to existing generic terminal states/reasons.
func ContributionTerminalMapping(outcome string) (string, string, bool) {
	switch outcome {
	case "succeeded":
		return "succeeded", "contribution_verified", true
	case "narrower-target-completed":
		return "succeeded", "contribution_narrower_completed", true
	case "failed":
		return "failed", "contribution_failed", true
	case "denied":
		return "denied", "contribution_denied", true
	case "cancelled":
		return "cancelled", "contribution_cancelled", true
	case "expired":
		return "expired", "contribution_expired", true
	case "exhausted":
		return "failed", "contribution_exhausted", true
	case "unknown":
		return "failed", "contribution_unknown", true
	case "merged-unresolved":
		return "failed", "contribution_merged_unresolved", true
	}
	return "", "", false
}
