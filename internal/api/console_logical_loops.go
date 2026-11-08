package api

import (
	"fmt"
	"sort"

	"github.com/berryhill/aegis/internal/app"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func consoleLoopActiveVersion(view app.LoopView) string {
	if view.Lifecycle.State != "active" || view.Lifecycle.ActiveRevision == 0 {
		return "None"
	}
	return exactRevisionLabel(view.Revision.LoopID, view.Lifecycle.ActiveRevision, view.Lifecycle.ActiveDigest)
}

// Group only the Console discovery projection. API inventory and exact inspection
// retain every immutable revision; Graph and Queue joins still use raw inventory.
func groupConsoleLoops(model *consoleweb.SurfaceModel) {
	model.ExactLoopRecords = model.Records
	groups := map[string][]consoleweb.RecordModel{}
	order := []string{}
	for _, record := range model.ExactLoopRecords {
		id := record.Loop.StableID
		if _, exists := groups[id]; !exists {
			order = append(order, id)
		}
		groups[id] = append(groups[id], record)
	}
	model.Records = nil
	for _, id := range order {
		revisions := groups[id]
		sort.Slice(revisions, func(i, j int) bool {
			return revisions[i].Loop.PublicationRevision > revisions[j].Loop.PublicationRevision
		})
		history := make([]consoleweb.LinkModel, 0, len(revisions))
		for _, revision := range revisions {
			history = append(history, consoleweb.LinkModel{Label: revision.Revision + " · " + revision.Loop.HumanTitle, Detail: revision.Digest + " · exact diagram and publication provenance", URL: consoleRecordURL(consoleLoops, revision.Key)})
		}
		for _, revision := range revisions {
			revision.Loop.RevisionHistory = history
		}
		head := revisions[0]
		head.Lifecycle = head.Loop.LogicalLifecycle
		head.Summary = fmt.Sprintf("%s · latest publication %s · active revision %s · %d immutable revisions", head.Loop.HumanTitle, head.Revision, head.Loop.ActiveVersion, len(revisions))
		model.Records = append(model.Records, head)
	}
	model.TotalCount, model.TotalRecords = len(model.Records), len(model.Records)
	model.Status = fmt.Sprintf("%d authoritative logical Loops · immutable revision history retained · source %s", len(model.Records), model.Source)
}
