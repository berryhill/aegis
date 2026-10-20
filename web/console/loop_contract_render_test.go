package consoleweb

import (
	"context"
	"html"
	"strings"
	"testing"
)

func TestLoopContractRenderAssertions(t *testing.T) {
	empty, hostile := "", "\n \n<script>alert('x')</script>\n "
	long := strings.Repeat("x", 4096)
	for _, tc := range []struct {
		name, mode string
		expected   *string
	}{
		{"nil", "presence-only", nil}, {"empty", "trimmed-text", &empty},
		{"newlines", "exact-bytes", &hostile}, {"trimmed", "trimmed-text", &hostile},
		{"long", "exact-bytes", &long},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail := &LoopDetailModel{DoerV4: true, DoerContract: &LoopDoerContractModel{Task: "task <untrusted>", Workspace: "/workspace", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", MaxAttempts: 2, Digest: "contract-digest", VerificationMode: tc.mode, ExpectedText: tc.expected}}
			record := &RecordModel{Key: "contract:1", Label: "contract", Revision: "r1", Loop: detail}
			var out strings.Builder
			if err := LoopWorkspace(SurfaceModel{Domain: DomainLoops}, record, buildLoopTopology(detail)).Render(context.Background(), &out); err != nil {
				t.Fatal(err)
			}
			rendered := out.String()
			for _, want := range []string{"Sealed v4 task contract", "task &lt;untrusted&gt;", "/workspace", "result.txt", "Maximum attempts", "contract-digest", "<dd>" + tc.mode + "</dd>", "not actual artifact content"} {
				if strings.Count(rendered, want) < 2 {
					t.Errorf("both displays must contain %q", want)
				}
			}
			if strings.Contains(rendered, "<script>") {
				t.Fatal("unescaped expected text")
			}
			if tc.expected == nil {
				if strings.Count(rendered, "Not specified (presence-only).") != 2 || strings.Contains(rendered, "data-loop-expected-text") {
					t.Fatal("nil expectation lost")
				}
			} else {
				if strings.Count(rendered, `data-loop-expected-text><code>`+html.EscapeString(*tc.expected)+`</code></pre>`) != 2 {
					t.Fatalf("expected bytes not preserved: %q", *tc.expected)
				}
				if *tc.expected == "" && strings.Count(rendered, "Explicitly empty text (0 bytes).") != 2 {
					t.Fatal("empty expectation lost")
				}
			}
			if !strings.Contains(rendered, `id="loop-panel-evidence" class="lw-tab-panel" data-loop-tab-panel="evidence" aria-labelledby="loop-tab-evidence">`) {
				t.Fatal("no-JS evidence missing or hidden")
			}
			if !strings.Contains(rendered, "Possible terminal outcomes") || !strings.Contains(rendered, "not actual execution disposition") {
				t.Fatal("missing declared outcomes disclaimer")
			}
			if strings.Contains(rendered, "No Loop inputs") || strings.Contains(rendered, "Required inputs") {
				t.Fatal("misleading port copy")
			}
		})
	}
}

func TestLoopContractRenderReusableAndGeneric(t *testing.T) {
	for _, reusable := range []bool{false, true} {
		detail := &LoopDetailModel{DoerV5: reusable}
		if reusable {
			detail.Inputs = []LoopPortModel{{ID: "task", Type: "string", Required: true}, {ID: "expected_text", Type: "string"}}
		}
		var out strings.Builder
		if err := LoopDefinitionContract(detail).Render(context.Background(), &out); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		if strings.Contains(text, "Sealed v4") || strings.Contains(text, "Pre-run contract digest") {
			t.Fatal("fabricated sealed contract")
		}
		if !strings.Contains(text, "No declared dataflow output ports.") {
			t.Fatal("missing honest empty output")
		}
		if reusable {
			for _, want := range []string{"concrete task values are supplied per run", "task", "expected_text", "Required", "Optional"} {
				if !strings.Contains(text, want) {
					t.Errorf("missing %q", want)
				}
			}
		} else if !strings.Contains(text, "No declared dataflow input ports.") || strings.Contains(text, "sealed in this revision") {
			t.Fatal("generic copy is Doer-specific")
		}
	}
}
