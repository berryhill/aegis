package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func loopPanelElement(n *html.Node, key, value string) *html.Node {
	if n.Type == html.ElementNode {
		for _, attr := range n.Attr {
			if attr.Key == key && attr.Val == value {
				return n
			}
		}
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := loopPanelElement(child, key, value); found != nil {
			return found
		}
	}
	return nil
}

func loopPanelText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var result strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			result.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return result.String()
}

func TestLoopRequiredInputsPanelIsInputFocused(t *testing.T) {
	for _, sealed := range []bool{false, true} {
		detail := &LoopDetailModel{Inputs: []LoopPortModel{{ID: "document", Type: "string", Required: true}, {ID: "optional", Type: "boolean"}}}
		if sealed {
			detail.DoerV4 = true
			detail.Inputs = nil
			detail.DoerContract = &LoopDoerContractModel{Task: "sealed task for contextual inspection", Workspace: "/synthetic-fixture", VerifyFile: "result.txt", WritableFiles: []string{"result.txt"}, MaxAttempts: 2}
		}
		record := &RecordModel{Key: "loop-input:1", Label: "loop-input", Revision: "r1", Loop: detail}
		var out bytes.Buffer
		if err := LoopWorkspace(SurfaceModel{}, record, buildLoopTopology(detail)).Render(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		doc, err := html.Parse(strings.NewReader(out.String()))
		if err != nil {
			t.Fatal(err)
		}
		panel := loopPanelElement(doc, "id", "loop-panel-contract")
		if panel == nil {
			t.Fatal("required-inputs tab panel missing")
		}
		if loopPanelElement(panel, "class", "loop-support-section loop-sealed-contract") != nil {
			t.Fatal("sealed contract displaced the input-focused supporting tab")
		}
		if sealed {
			contextual := loopPanelElement(doc, "data-loop-definition-panel", "")
			if !strings.Contains(loopPanelText(contextual), detail.DoerContract.Task) {
				t.Fatal("sealed contract disappeared from contextual definition details")
			}
		} else {
			table := loopPanelElement(panel, "aria-label", "Declared Loop inputs")
			if table == nil || table.Data != "table" {
				t.Fatal("required-input panel lacks the accessible accepted-input table")
			}
			text := loopPanelText(table)
			for _, want := range []string{"document", "string", "Required", "optional", "boolean", "Optional", "Not declared"} {
				if !strings.Contains(text, want) {
					t.Fatalf("input table lost authoritative or unavailable field %q", want)
				}
			}
		}
	}
}
