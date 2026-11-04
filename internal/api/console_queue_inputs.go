package api

import (
	"github.com/berryhill/aegis/internal/app"
	consoleweb "github.com/berryhill/aegis/web/console"
)

// buildQueueNormalizedInputs projects only the inputs already disclosed by the
// authenticated QueueExecutionView (including its existing Console JSON view).
// It does not load definition source, workspace contents or credentials.
func buildQueueNormalizedInputs(detail *consoleweb.QueueDetailModel, view app.QueueExecutionView) {
	for _, input := range view.Inputs {
		// NormalizedInput retains canonical JSON, not caller/default provenance.
		detail.Inputs = append(detail.Inputs, consoleweb.QueueInputModel{
			PortID: input.PortID, Type: string(input.Type), Value: string(input.Value),
			Source: "normalized run snapshot", Status: "applied",
		})
	}
}
