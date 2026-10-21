package api

import (
	"context"
	"fmt"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	consoleweb "github.com/berryhill/aegis/web/console"
)

// Metadata visibility does not grant content visibility. Join only the selected
// Queue item, through the same pinned-owner/evidence reader as the typed CLI.
func consoleQueueRunResult(ctx context.Context, svc *app.Service, subject core.Subject, id string) *consoleweb.QueueRunResultModel {
	result, err := svc.GetQueueResultAs(ctx, subject, id)
	if err != nil {
		return &consoleweb.QueueRunResultModel{Notice: "Named result unavailable: exact owner authorization and intact verified evidence are required."}
	}
	if result.Reason == "result_source_unsupported" {
		return nil
	}
	model := &consoleweb.QueueRunResultModel{Task: result.Task, Status: string(result.Status), Verification: result.Verification, Usage: result.Usage}
	model.Fields = []consoleweb.FieldModel{{Label: "Queue item", Value: result.QueueItem.ID + " @ " + result.QueueItem.Digest}, {Label: "Run", Value: result.GraphRunID}, {Label: "Loop", Value: exactRevisionLabel(result.Source.ID, result.Source.Revision, result.Source.Digest)}}
	if result.Elapsed != nil {
		model.Fields = append(model.Fields, consoleweb.FieldModel{Label: "Elapsed (includes Queue waiting)", Value: time.Duration(result.Elapsed.Nanoseconds).String()}, consoleweb.FieldModel{Label: "Duration source", Value: result.Elapsed.Source})
	}
	if result.Result == nil {
		model.Notice = "No verified result is available for this execution."
		return model
	}
	selected := result.Result
	model.Verified, model.Name, model.Content, model.Encoding = true, selected.Name, selected.Content, selected.Encoding
	model.Fields = append(model.Fields,
		consoleweb.FieldModel{Label: "Agent", Value: exactRevisionLabel(selected.Participant.ID, selected.Participant.Revision, selected.Participant.Digest)},
		consoleweb.FieldModel{Label: "Loop execution", Value: selected.LoopExecutionID},
		consoleweb.FieldModel{Label: "Attempt", Value: selected.AttemptID},
		consoleweb.FieldModel{Label: "Contract digest", Value: selected.ContractDigest},
		consoleweb.FieldModel{Label: "Artifact", Value: selected.Artifact.ID + " @ " + selected.Artifact.Digest},
		consoleweb.FieldModel{Label: "Verification receipt", Value: selected.Receipt.ID + " · " + string(selected.Receipt.Outcome)},
		consoleweb.FieldModel{Label: "Stored file", Value: selected.FileRef},
		consoleweb.FieldModel{Label: "Content metadata", Value: fmt.Sprintf("%s · %s · %d bytes", selected.MediaType, selected.Encoding, selected.SizeBytes)})
	return model
}
