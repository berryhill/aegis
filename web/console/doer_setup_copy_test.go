package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestDoerSetupVisibleCopyAndHeadlessContinuation(t *testing.T) {
	var out bytes.Buffer
	if err := DoerSetupPage(DoerSetupModel{DraftID: "internal-task-id", Version: "7", Contract: "retained task", HostStatus: "review required"}, "csrf").Render(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	var visible strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			visible.WriteString(n.Data)
			visible.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	text := visible.String()
	if strings.Contains(strings.ToLower(text), "draft") {
		t.Fatal("storage terminology leaked into visible setup")
	}
	for _, want := range []string{"Set up this Loop", "No desktop companion is required", "exact version:", "loops task-show ID", "original Run idempotency key", "Setup is not task completion"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(out.String(), "/doer/continuation") {
		t.Fatal("setup requires another task-consent/companion detour")
	}
	if stateHeading("draft") != "Not activated" {
		t.Fatal("internal lifecycle leaked")
	}
}
