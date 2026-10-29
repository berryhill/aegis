package delivery

// ClassifyContributionNonSuccess uses only independently validated canonical
// history. Earlier failures remain history; only current effects set the cause.
// Any retained irreversible merge upgrades a later non-success to unresolved.
func ClassifyContributionNonSuccess(h HistorySnapshot, candidate string) (string, string) {
	if ValidateHistorySnapshot(h) != nil {
		return "", ""
	}
	merged := false
	outcome, cause := "", ""
	selected := 0
	for _, chain := range h.Histories {
		for _, e := range chain {
			merged = merged || e.Record.Merge != ""
		}
		r := chain[len(chain)-1].Record
		if r.Intent.Candidate != candidate || r.Outcome == Confirmed {
			continue
		}
		rank := 1
		switch r.Intent.Operation {
		case Publish, TransferGraph, UploadBlob, UploadTree, UploadCommit:
			rank = 2
		case PullRequest, UpdatePullRequest:
			rank = 3
		case CheckDelivery:
			rank = 4
		case Merge:
			rank = 5
		case ReconcileIssue:
			rank = 6
		}
		if rank < selected {
			continue
		}
		selected = rank
		switch r.Outcome {
		case Failed:
			outcome, cause = "failed", "delivery_failed"
		case Denied:
			outcome, cause = "denied", "authority_denied"
		case Unknown:
			outcome, cause = "unknown", "effect_unknown"
		default:
			return "", ""
		}
		switch r.Intent.Operation {
		case CheckDelivery:
			if r.Code == "ci_failed" {
				cause = "ci_failed"
			} else {
				cause = "checks_unresolved"
			}
		case Merge:
			cause = "merge_unresolved"
		case ReconcileIssue:
			cause = "issue_unresolved"
		}
	}
	if outcome != "" && merged {
		outcome = "merged-unresolved"
	}
	return outcome, cause
}
