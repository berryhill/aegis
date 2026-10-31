package implementation

import "regexp"

// ContributionFailureFeedback is bounded data, never a command or remote prose.
// No log, annotation, URL, reproduction command or suggested instruction is copied.
// The owning controller derives it from the exact retained HTTP proof and policy.
type ContributionFailureFeedback struct {
	Kind           string `json:"kind"`
	Check          string `json:"check,omitempty"`
	Expected       string `json:"expected"`
	Observed       string `json:"observed"`
	ResponseDigest string `json:"response_digest"`
}

var feedbackName = regexp.MustCompile(`^[A-Za-z0-9_./:@+ ()\[\]~-]{1,128}$`)

func (f ContributionFailureFeedback) Valid(class string) bool {
	if !sha256Digest(f.ResponseDigest) {
		return false
	}
	if class == "merge_conflict" {
		return f.Kind == "pull_mergeability" && f.Check == "" && f.Expected == "mergeable_clean" && f.Observed == "mergeable_false_dirty"
	}
	if class != "ci_failed" || !feedbackName.MatchString(f.Check) || f.Expected != "success" {
		return false
	}
	if f.Kind == "check_run" {
		return f.Observed == "completed_failure" || f.Observed == "completed_timed_out" || f.Observed == "completed_cancelled" || f.Observed == "completed_action_required" || f.Observed == "completed_startup_failure" || f.Observed == "completed_stale"
	}
	return f.Kind == "commit_status" && (f.Observed == "failure" || f.Observed == "error")
}
