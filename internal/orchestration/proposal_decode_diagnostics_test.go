package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/implementation"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/looprun"
	hermesruntime "github.com/berryhill/aegis/internal/runtime/hermes"
)

func TestProposalDecodeClosedReasons(t *testing.T) {
	good := `{"edits":[{"path":"selected.txt","content":"eA=="}],"report":"proposed"}`
	cases := []struct{ name, wire, code string }{
		{"empty", "", "size_invalid"},
		{"bound", strings.Repeat("x", hermesruntime.MaxAttemptOutputBytes+1), "size_invalid"},
		{"syntax", "private model/provider text /private/path credential-canary", "json_invalid"},
		{"markdown", "```json\n" + good + "\n```", "json_invalid"},
		{"truncated", `{"edits":[`, "json_invalid"},
		{"duplicate", strings.Replace(good, `"report":"proposed"`, `"report":"private","report":"other"`, 1), "duplicate_field"},
		{"nested duplicate", strings.Replace(good, `"path":"selected.txt"`, `"path":"private","pa\u0074h":"other"`, 1), "duplicate_field"},
		{"unknown", strings.Replace(good, `"report":`, `"private credential-canary":true,"report":`, 1), "unknown_field"},
		{"nested unknown", strings.Replace(good, `"content":`, `"content_base64":"private","content":`, 1), "unknown_field"},
		{"case alias", strings.Replace(good, `"path":`, `"Path":`, 1), "unknown_field"},
		{"trailing", good + ` {}`, "trailing_data"},
		{"trailing prose", good + ` private credential-canary`, "trailing_data"},
		{"base64", strings.Replace(good, "eA==", "private-not-base64!", 1), "base64_invalid"},
		{"root null", `null`, "shape_invalid"},
		{"root array", `[]`, "shape_invalid"},
		{"content null", strings.Replace(good, `"eA=="`, `null`, 1), "shape_invalid"},
		{"content array", strings.Replace(good, `"eA=="`, `[120]`, 1), "shape_invalid"},
		{"missing content", `{"edits":[{"path":"selected.txt"}],"report":"done"}`, "shape_invalid"},
		{"report type", strings.Replace(good, `"proposed"`, `null`, 1), "shape_invalid"},
		{"edits empty", `{"edits":[],"report":"done"}`, "edits_invalid"},
		{"edits bound", `{"edits":[` + strings.TrimSuffix(strings.Repeat(`{"path":"selected.txt","content":"eA=="},`, 129), ",") + `],"report":"done"}`, "edits_invalid"},
		{"report absent", `{"edits":[{"path":"selected.txt","content":"eA=="}]}`, "report_invalid"},
		{"report blank", strings.Replace(good, `"proposed"`, `" \n\t"`, 1), "report_invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := decodeReportedPatch([]byte(tc.wire))
			want := "doer_proposal_" + tc.code
			var failure *looprun.Failure
			if !errors.As(err, &failure) || failure.Code != want || err.Error() != want || len(p.Edits) != 0 || p.Report != "" {
				t.Fatalf("closed failure mismatch: %+v %v", failure, err)
			}
			wire, _ := json.Marshal(failure)
			for _, canary := range []string{"private", "credential-canary", "provider"} {
				if strings.Contains(string(wire), canary) {
					t.Fatal("content escaped")
				}
			}
		})
	}
	p, err := decodeReportedPatch([]byte(good))
	if err != nil || len(p.Edits) != 1 || p.Edits[0].Path != "selected.txt" || string(p.Edits[0].Content) != "x" || p.Report != "proposed" {
		t.Fatal("exact schema failed", err)
	}
}

func TestPatchResponseInstructionMatchesWire(t *testing.T) {
	for _, reported := range []bool{false, true} {
		instruction := patchResponseInstruction(reported)
		start := strings.Index(instruction, `{"edits":`)
		end := strings.Index(instruction[start:], ". Placeholders") + start
		shape := instruction[start:end]
		shape = strings.ReplaceAll(shape, "<approved relative path>", "selected.txt")
		shape = strings.ReplaceAll(shape, "<standard base64 of complete file bytes>", "eA==")
		shape = strings.ReplaceAll(shape, "<nonempty implementation summary>", "proposed")
		p, err := decodePatch([]byte(shape), reported)
		if err != nil || len(p.Edits) != 1 || string(p.Edits[0].Content) != "x" {
			t.Fatal("instruction does not describe decoder", err)
		}
		for _, required := range []string{"No Markdown", "duplicate keys", "standard padded base64", "1 to 128", "writable_files", "no implicit newline", "independently"} {
			if !strings.Contains(instruction, required) {
				t.Fatalf("missing constraint %s", required)
			}
		}
	}
}

func TestDecodedProposalScopeStillDenied(t *testing.T) {
	expected := "x"
	contract := loop.DoerContract{Task: "write selected file", Workspace: t.TempDir(), WritableFiles: []string{"selected.txt"}, VerifyFile: "selected.txt", ExpectedText: &expected, MaxAttempts: 2}
	for _, path := range []string{"../escape.txt", "/escape.txt", "unapproved.txt"} {
		wire, _ := json.Marshal(implementation.Proposal{Edits: []implementation.Edit{{Path: path, Content: []byte("x")}}, Report: "proposed"})
		p, err := decodeReportedPatch(wire)
		if err != nil {
			t.Fatal(err)
		}
		if implementation.ApplyDoerEdits(context.Background(), contract, p.Edits, func(context.Context, string) error { return nil }) == nil {
			t.Fatal("scope escape accepted")
		}
	}
}
