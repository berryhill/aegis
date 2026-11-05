package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLoopLifecyclePreviewUsesAuthenticatedSurfaceCSRF(t *testing.T) {
	record := &RecordModel{Key: "loop-control:1", Label: "loop-control", Revision: "r1", Digest: "exact-digest", Loop: &LoopDetailModel{
		TargetID: "loop-control:1", Digest: "exact-digest", PublisherID: "publisher-not-a-session-csrf", CanActivate: true, CanRetire: true,
	}}
	var output bytes.Buffer
	if err := LoopWorkspace(SurfaceModel{CSRF: "session-csrf-marker"}, record, buildLoopTopology(record.Loop)).Render(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	if strings.Count(body, `name="csrf" value="session-csrf-marker"`) != 2 {
		t.Fatal("lifecycle preview forms do not carry the authenticated surface CSRF")
	}
	if strings.Contains(body, `name="csrf" value="publisher-not-a-session-csrf"`) {
		t.Fatal("publisher identity was substituted for the session CSRF")
	}
}
