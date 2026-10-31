package delivery

import "github.com/berryhill/aegis/internal/implementation"

// ConcreteRepairFeedback projects only policy-selected identifiers and closed
// observed states from canonical readback. Missing/ambiguous facts deny repair;
// logs, annotations and remote suggestions never enter the native builder.
func ConcreteRepairFeedback(r Record, required []string) (implementation.ContributionFailureFeedback, error) {
	var out implementation.ContributionFailureFeedback
	if !repairableFailure(r) {
		return out, ErrDenied
	}
	p, err := ParseReadback(r.Intent, r.Observation)
	if err != nil {
		return out, ErrDenied
	}
	for _, f := range p.Facts {
		if f.HTTPStatus != 200 || f.Target.Repository != r.Intent.Repository {
			continue
		}
		if r.Code == "merge_conflict" {
			if f.Kind == "pull" && f.Target.PR == r.Intent.PR && f.Number == r.Intent.PR && f.Head != nil && f.Head.SHA == r.Intent.Head && f.Base != nil && f.Base.SHA == r.Intent.BaseHead && f.Mergeable != nil && !*f.Mergeable && f.MergeableState == "dirty" {
				out = implementation.ContributionFailureFeedback{Kind: "pull_mergeability", Expected: "mergeable_clean", Observed: "mergeable_false_dirty", ResponseDigest: f.ResponseDigest}
				return out, nil
			}
			continue
		}
		if f.Target.Head != r.Intent.Head || f.HeadSHA != r.Intent.Head || f.ID == 0 {
			continue
		}
		for _, name := range required {
			candidate := implementation.ContributionFailureFeedback{Kind: f.Kind, Check: name, Expected: "success", ResponseDigest: f.ResponseDigest}
			if f.Kind == "check_run" && f.Name == name && f.App != nil && f.App.ID != 0 {
				candidate.Observed = f.Status + "_" + f.Conclusion
			}
			if f.Kind == "commit_status" && f.Context == name && f.Creator != nil && f.Creator.ID != 0 {
				candidate.Observed = f.State
			}
			if candidate.Valid(r.Code) {
				return candidate, nil
			}
		}
	}
	return out, ErrDenied
}
