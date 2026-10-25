package delivery

// validateObservation validates every supplied fact against the exact reserved
// operation and accepted plan. Omitted facts are not negative evidence.
func validateObservation(i Intent, p Plan, o Observation) error {
	for _, value := range []string{o.Head, o.Tree, o.Merge, o.BaseHead} {
		if value != "" && !sha.MatchString(value) {
			return ErrDenied
		}
	}
	head := i.Head
	if i.Operation == UploadBlob || i.Operation == UploadTree || i.Operation == UploadCommit {
		head = p.Object.OID
	}
	if o.Head != "" && o.Head != head {
		return ErrConflict
	}
	if o.Tree != "" && (p.Commit.Tree == "" || o.Tree != p.Commit.Tree) {
		return ErrConflict
	}
	if (o.Merge != "" || o.BaseHead != "") && i.Operation != Merge {
		return ErrDenied
	}
	if o.PR != 0 {
		switch i.Operation {
		case PullRequest, UpdatePullRequest, Merge, ReconcileIssue:
		default:
			return ErrDenied
		}
		if i.PR != 0 && o.PR != i.PR {
			return ErrConflict
		}
	}
	return nil
}

// retainObservation joins immutable facts, never replacing them with absence.
// BaseHead is deliberately the FIRST observed historical base, not a mutable
// projection of today's branch tip. Adapters must still prove current containment
// on every readback before confirmation; base advancement cannot invalidate the
// historical fact or change the exact reserved precondition.
func retainObservation(old Record, o Observation) (Observation, error) {
	for _, pair := range []struct {
		old  string
		next *string
	}{{old.Head, &o.Head}, {old.Tree, &o.Tree}, {old.Merge, &o.Merge}} {
		if pair.old != "" {
			if *pair.next != "" && *pair.next != pair.old {
				return Observation{}, ErrConflict
			}
			*pair.next = pair.old
		}
	}
	if old.PR != 0 {
		if o.PR != 0 && o.PR != old.PR {
			return Observation{}, ErrConflict
		}
		o.PR = old.PR
	}
	if old.BaseHead != "" {
		o.BaseHead = old.BaseHead
	}
	if o.Merge != "" && o.Outcome != Confirmed {
		o.Outcome, o.Code = Unknown, "merged_validation_unresolved"
	}
	return o, nil
}
