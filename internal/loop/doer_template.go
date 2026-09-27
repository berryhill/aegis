package loop

import "encoding/json"

// DoerTemplate describes an included authoring recipe, not a published Loop,
// canonical revision, authority grant, or runnable instance. Operator inputs
// must be supplied and validated by NewDoerRevision before publication review.
type DoerTemplate struct {
	ID             string   `json:"id"`
	Version        string   `json:"version"`
	Digest         string   `json:"digest"`
	Description    string   `json:"description"`
	RequiredInputs []string `json:"required_inputs"`
}

const DoerTemplateID = "aegis.doer.selected-file"
const DoerTemplateVersion = "v1"

// IncludedDoerTemplate returns an independent descriptor for the built-in
// selected-file Doer recipe. It does not consult or mutate fleet state.
func IncludedDoerTemplate() DoerTemplate {
	template := DoerTemplate{
		ID: DoerTemplateID, Version: DoerTemplateVersion,
		Description:    "Build a bounded Doer Loop v4 draft for one operator-selected task and independently verified workspace file; publication and execution require separate authority.",
		RequiredInputs: []string{"agent_id", "loop_id", "revision", "idempotency_key", "doer.task", "doer.workspace", "doer.writable_files", "doer.verify_file", "doer.max_attempts"},
	}
	// The digest identifies these static recipe bytes; it is not a revision,
	// authority context, or execution contract digest.
	wire, _ := json.Marshal(template)
	template.Digest = sha256Digest(wire)
	return template
}
