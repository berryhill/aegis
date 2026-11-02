package delivery

// VerifiedBaseAdvance acknowledges a known read-only prerequisite, not an
// unknown mutation. The original Unknown envelope is never rewritten.
func VerifiedBaseAdvance(r Record) (string, error) {
	if r.Intent.Operation != CheckDelivery || r.Revision < 2 || r.Outcome != Unknown || r.Observation.Outcome != Unknown || r.Code != "base_advanced" || r.Merge != "" || r.Intent.PR == 0 {
		return "", ErrDenied
	}
	p, err := ParseReadback(r.Intent, r.Observation)
	if err != nil {
		return "", ErrDenied
	}
	current := ""
	for _, f := range p.Facts {
		if f.Kind == "pull" && f.HTTPStatus == 200 && f.Target.Repository == r.Intent.Repository && f.Target.PR == r.Intent.PR && f.Number == r.Intent.PR && !f.Merged && f.State == "open" && f.Head != nil && f.Head.SHA == r.Intent.Head && f.Head.Repo.FullName == r.Intent.Repository && f.Base != nil && f.Base.Repo.FullName == r.Intent.Repository && f.Base.Ref == r.Intent.Base && sha.MatchString(f.Base.SHA) && f.Base.SHA != r.Intent.BaseHead {
			current = f.Base.SHA
		}
	}
	if current == "" {
		return "", ErrDenied
	}
	base, head, ancestry := false, false, false
	for _, f := range p.Facts {
		if f.HTTPStatus != 200 || f.Target.Repository != r.Intent.Repository {
			continue
		}
		if f.Kind == "ref" && f.Target.Ref == r.Intent.Base && f.Object != nil && f.Object.SHA == current {
			base = true
		}
		if f.Kind == "ref" && f.Target.Ref == r.Intent.Branch && f.Object != nil && f.Object.SHA == r.Intent.Head {
			head = true
		}
		if f.Kind == "comparison" && f.Target.ComparisonBase == r.Intent.BaseHead && f.Target.ComparisonHead == current && f.Status == "ahead" && f.Behind == 0 {
			ancestry = true
		}
	}
	if !base || !head || !ancestry {
		return "", ErrDenied
	}
	return current, nil
}
