package api

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	consoleweb "github.com/berryhill/aegis/web/console"
	"golang.org/x/net/html"
)

func loopMaximumPresentationFixture(t *testing.T) (loop.LoopRevision, loop.LoopValidationResult) {
	t.Helper()
	input := loop.LoopRevision{LoopID: "maximum-loop", Name: "Maximum bounded definition", Revision: 1, EntryStepID: "s0"}
	for i := 0; i < loop.MaxSteps; i++ {
		step := loop.Step{ID: fmt.Sprintf("s%d", i), Kind: loop.StepAction, Retry: loop.RetryPolicy{MaxAttempts: 1}}
		if i == loop.MaxSteps-1 {
			step.Kind, step.Terminal = loop.StepTerminal, &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}
		}
		input.Steps = append(input.Steps, step)
	}
	add := func(from, to int) {
		input.Transitions = append(input.Transitions, loop.Transition{ID: fmt.Sprintf("e%d-%d", from, to), FromStepID: input.Steps[from].ID, ToStepID: input.Steps[to].ID, Condition: fmt.Sprintf("branch-%d", to)})
	}
	for i := 0; i < loop.MaxSteps-1; i++ {
		add(i, i+1)
	}
	for from := 0; from < loop.MaxSteps-1 && len(input.Transitions) < loop.MaxTransitions; from++ {
		for to := from + 2; to < loop.MaxSteps && len(input.Transitions) < loop.MaxTransitions; to++ {
			add(from, to)
		}
	}
	for i := range input.Steps {
		count := 0
		for _, edge := range input.Transitions {
			if edge.FromStepID == input.Steps[i].ID {
				count++
			}
		}
		if count > 1 {
			input.Steps[i].Kind, input.Steps[i].Gate = loop.StepGate, &loop.GateDefinition{Mode: "exclusive"}
		}
	}
	revision, validation, err := loop.NewRevision(input)
	if err != nil || validation.Outcome != loop.ValidationValid {
		t.Fatalf("maximum fixture did not pass actual canonical validation: %v %+v", err, validation.Issues)
	}
	if len(revision.Steps) != loop.MaxSteps || len(revision.Transitions) != loop.MaxTransitions {
		t.Fatal("maximum fixture lost exact admitted bounds")
	}
	return revision, validation
}

func TestConsoleLoopMaximumDefinitionFitsExistingRenderBound(t *testing.T) {
	revision, validation := loopMaximumPresentationFixture(t)
	record := consoleLoopRecord(app.LoopView{Revision: revision, Validations: []loop.LoopValidationResult{validation}})
	model := consoleweb.PageModel{Authenticated: true, Surface: consoleweb.SurfaceModel{Domain: string(consoleLoops), State: "ready", Authoritative: true, InspectorOpen: true, Inspector: &record, Records: []consoleweb.RecordModel{record}}}
	var raw bytes.Buffer
	if err := consoleweb.Document(model).Render(context.Background(), &raw); err != nil {
		t.Fatal(err)
	}
	t.Logf("maximum response bytes=%d existing bound=%d", raw.Len(), maxConsolePatchBytes)
	doc, parseErr := html.Parse(bytes.NewReader(raw.Bytes()))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	stats := map[string]int{}
	panels, canvasNodes, paths, labels, inventory := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	attribute := func(n *html.Node, key string) string {
		for _, attr := range n.Attr {
			if attr.Key == key {
				return attr.Val
			}
		}
		return ""
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		for _, attr := range n.Attr {
			if attr.Key == "data-loop-node-panel" {
				panels[attr.Val]++
			}
			if attr.Key == "data-loop-node" {
				canvasNodes[attr.Val]++
			}
			if attr.Key == "data-transition-id" {
				if n.Data == "path" {
					paths[attr.Val]++
				}
				if n.Data == "text" {
					labels[attr.Val]++
				}
			}
			key := ""
			if attr.Key == "class" && (attr.Val == "loop-textual" || attr.Val == "loop-visible-transitions" || attr.Val == "loop-edge-layer") {
				key = attr.Val
			} else if attr.Key == "data-loop-node-panel" {
				key = "node-panels"
			}
			if key != "" {
				var section bytes.Buffer
				if err := html.Render(&section, n); err != nil {
					t.Fatal(err)
				}
				stats[key] += section.Len()
			}
		}
		if n.Data == "li" && n.Parent != nil && strings.Contains(" "+attribute(n.Parent, "class")+" ", " loop-visible-transitions ") {
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if child.Data == "strong" && child.FirstChild != nil {
					inventory[child.FirstChild.Data]++
					break
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	t.Logf("response sections=%v", stats)
	if len(panels) != loop.MaxSteps || len(canvasNodes) != loop.MaxSteps || len(paths) != loop.MaxTransitions || len(labels) != loop.MaxTransitions || len(inventory) != loop.MaxTransitions {
		t.Fatal("response-size repair dropped or duplicated maximum-definition content")
	}
	for i := range revision.Steps {
		if panels[strconv.Itoa(i)] != 1 || canvasNodes[strconv.Itoa(i)] != 1 {
			t.Fatal("step contract must have exactly one copy and one canvas control")
		}
	}
	for _, edge := range revision.Transitions {
		if paths[edge.ID] != 1 || labels[edge.ID] != 1 || inventory[edge.ID] != 1 {
			t.Fatal("stored edge missing or duplicated in canvas/textual equivalent")
		}
	}
	body, err := renderConsole(context.Background(), consoleweb.Document(model))
	if err != nil {
		t.Fatalf("admitted maximum definition cannot be inspected under the unchanged response bound: %v", err)
	}
	if len(body) > maxConsolePatchBytes {
		t.Fatal("renderer exceeded its existing response bound")
	}
}
