package consoleweb

import (
	"context"
	"strings"
	"testing"
)

func TestGraphWorkspaceRenderInlineStyle(t *testing.T) {
	g := &GraphDetailModel{Nodes: []GraphNodeModel{{ID: "intake", Loop: "l1"}, {ID: "review", Loop: "l2"}}, Edges: []GraphEdgeModel{{ID: "next", From: "intake", To: "review"}}}
	r := &RecordModel{Key: "x", Label: "x", Revision: "r1", Lifecycle: "draft", Graph: g}
	var out strings.Builder
	if err := GraphDetailPage(SurfaceModel{Domain: DomainGraphs}, r).Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	t.Logf("contains stage style: %d", strings.Count(html, `data-graph-stage`))
	if i := strings.Index(html, `data-graph-stage`); i >= 0 {
		t.Logf("first stage tag: %q", html[i:i+200])
	}
}
