package app

import "github.com/berryhill/aegis/internal/graph"

// GraphWorkspaceFacts contains read-only structural and lifecycle facts for an
// exact revision. It does not establish submission eligibility or authority.
type GraphWorkspaceFacts struct {
	Validation        graph.GraphValidationResult
	StructurallyValid bool
	ActiveRevision    bool
}

// WorkspaceFacts derives current facts without replacing stored validation
// history or performing admission. Submission must still reload authority.
func (view GraphView) WorkspaceFacts() GraphWorkspaceFacts {
	validation := graph.ValidateRevision(view.Revision)
	return GraphWorkspaceFacts{
		Validation:        validation,
		StructurallyValid: validation.Outcome == graph.ValidationValid,
		ActiveRevision: view.Lifecycle.State == graph.LifecycleActive &&
			view.Lifecycle.ActiveRevision == view.Revision.Revision &&
			view.Lifecycle.ActiveDigest == view.Revision.Digest,
	}
}
