package api

import (
	"fmt"
	"strconv"

	"github.com/berryhill/aegis/internal/app"
	consoleweb "github.com/berryhill/aegis/web/console"
)

// enrichGraphWorkspace produces read-only facts, not an admission decision.
// Browser selection and canvas state never enter this projection or SubmitGraphAs.
func enrichGraphWorkspace(detail *consoleweb.GraphDetailModel, view app.GraphView, graphSets ...[]app.GraphView) {
	revision := view.Revision
	detail.GraphID = revision.GraphID
	detail.LatestVersion = "Unavailable"
	if len(graphSets) > 0 {
		var latest uint64
		for _, candidate := range graphSets[0] {
			if candidate.Revision.GraphID == revision.GraphID {
				latest = max(latest, candidate.Revision.Revision)
			}
		}
		if latest > 0 {
			detail.LatestVersion = fmt.Sprintf("r%d", latest)
			if latest == revision.Revision {
				detail.LatestVersion += " · viewing latest"
			} else {
				detail.LatestVersion += " · viewing historical revision"
			}
		}
	}
	facts := view.WorkspaceFacts()
	current := facts.Validation
	detail.CurrentValidation = string(current.Outcome) + " · " + current.Digest
	for _, issue := range current.Issues {
		detail.ValidationIssues = append(detail.ValidationIssues, consoleweb.GraphIssueModel{Code: issue.Code, Path: issue.Path, Message: issue.Message})
	}
	// Stored result is historical evidence, not silently relabelled current.
	detail.Validation = "Unavailable for this exact revision"
	for _, result := range view.Validations {
		if result.GraphID == revision.GraphID && result.Revision == revision.Revision && result.RevisionDigest == revision.Digest {
			detail.Validation = string(result.Outcome) + " · " + result.Digest
			break
		}
	}
	if !facts.StructurallyValid {
		detail.SubmissionIssues = append(detail.SubmissionIssues, consoleweb.GraphIssueModel{Code: "graph.invalid", Path: "revision", Message: "Not submittable: current structural validation failed. Preparation does not repair or authorize this definition."})
	}
	if !facts.ActiveRevision {
		detail.SubmissionIssues = append(detail.SubmissionIssues, consoleweb.GraphIssueModel{Code: "graph.inactive_revision", Path: "lifecycle", Message: "Not submittable: this exact revision is not the active Graph revision."})
	}
	detail.SubmissionIssues = append(detail.SubmissionIssues, consoleweb.GraphIssueModel{Code: "admission.not_evaluated", Path: "submission", Message: "Submittability is not established by this read. Exact Agent/Loop eligibility, typed inputs, runtime, mandate and server-derived authority are reloaded at submission."})
	for i, node := range revision.Nodes {
		projected := &detail.Nodes[i]
		projected.Links = []consoleweb.LinkModel{
			{Label: "Exact Agent", Detail: projected.Participant, URL: consoleAgentRevisionURL(node.Participant.ID, node.Participant.Revision)},
			{Label: "Exact Loop", Detail: projected.Loop, URL: consoleRecordURL(consoleLoops, node.Loop.ID+":"+strconv.FormatUint(node.Loop.Revision, 10))},
		}
		for _, mapping := range revision.InputMappings {
			if mapping.ToNodeID == node.ID {
				projected.InputMappings = append(projected.InputMappings, consoleweb.FieldModel{Label: mapping.GraphInput, Value: mapping.ToPort})
			}
		}
		for _, mapping := range revision.OutputMappings {
			if mapping.FromNodeID == node.ID {
				projected.OutputMappings = append(projected.OutputMappings, consoleweb.FieldModel{Label: mapping.FromPort, Value: mapping.GraphOutput})
			}
		}
	}
}
