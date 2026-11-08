package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLogicalLoopCollectionHistoryAndStableAvailability(t *testing.T) {
	record := RecordModel{Key: "stable:2", Label: "Untrusted human label", Revision: "r2", Availability: "published", AvailabilityHeadRevision: 2, AvailabilityHeadDigest: "head-digest", AvailabilitySequence: 7, AvailabilityDigest: "availability-digest", Loop: &LoopDetailModel{StableID: "stable/id", HumanTitle: "<authored title>", LogicalLifecycle: "active", ActiveVersion: "stable/id r1 @ first", RevisionHistory: []LinkModel{{Label: "r2", URL: "/console/loops?record_key=stable%3A2", Detail: "second"}, {Label: "r1", URL: "/console/loops?record_key=stable%3A1", Detail: "first"}}}}
	var output bytes.Buffer
	if err := RecordPanel(SurfaceModel{Domain: DomainLoops, Title: "Loops", CSRF: "csrf", Records: []RecordModel{record}}).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, want := range []string{"&lt;authored title&gt;", "Latest published revision · r2", "Active revision · stable/id r1", "Revision history · 2 immutable revisions", "ALL revisions", "does not stop execution", "loop_id=stable%2Fid", `name="revision" value="2"`, `name="digest" value="head-digest"`, `name="sequence" value="8"`, `name="previous_digest" value="availability-digest"`, `name="expected_state" value="published"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Count(body, `action="/console/loops/availability?`) != 1 || strings.Count(body, `data-record-key=`) != 1 {
		t.Fatal("logical row duplicated")
	}
	output.Reset()
	if err := LoopWorkspace(SurfaceModel{Domain: DomainLoops}, &record, buildLoopTopology(record.Loop)).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `action="/console/loops/availability?`) {
		t.Fatal("exact revision has logical mutation control")
	}
}
