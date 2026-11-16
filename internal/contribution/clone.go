package contribution

import "slices"

// Clone returns independently mutable contract data. Strings, time.Time and the
// reference/runtime/URL value structs have no caller-mutable interior state.
// Keep the recursive mutable-field inventory test in sync with schema changes.
func (c Contract) Clone() Contract {
	c.Invariants = slices.Clone(c.Invariants)
	c.Exclusions = slices.Clone(c.Exclusions)
	c.Roles = slices.Clone(c.Roles)
	c.Source.Discussion = slices.Clone(c.Source.Discussion)
	if c.Preparation != nil {
		p := *c.Preparation
		p.InventoryFiles = slices.Clone(p.InventoryFiles)
		p.Scope.WritableFiles = slices.Clone(p.Scope.WritableFiles)
		p.Scope.ContextFiles = slices.Clone(p.Scope.ContextFiles)
		p.Scope.Exclusions = slices.Clone(p.Scope.Exclusions)
		p.Inputs = slices.Clone(p.Inputs)
		p.Outputs = slices.Clone(p.Outputs)
		p.RuntimeSessionIDs = slices.Clone(p.RuntimeSessionIDs)
		p.InputBodies = slices.Clone(p.InputBodies)
		p.OutputBodies = slices.Clone(p.OutputBodies)
		c.Preparation = &p
	}
	if c.Engineering != nil {
		e := *c.Engineering
		e.WritableFiles = slices.Clone(e.WritableFiles)
		e.Policy.RequiredTests = slices.Clone(e.Policy.RequiredTests)
		e.Policy.Packages = slices.Clone(e.Policy.Packages)
		if e.RepositoryContext != nil {
			r := *e.RepositoryContext
			r.Files = slices.Clone(r.Files)
			e.RepositoryContext = &r
		}
		if e.Contribution != nil {
			r := *e.Contribution
			e.Contribution = &r
		}
		c.Engineering = &e
	}
	return c
}
