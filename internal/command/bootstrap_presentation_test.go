package command

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/onboarding"
	"github.com/berryhill/aegis/internal/tui"
	"github.com/spf13/cobra"
)

func TestBootstrapPresentationDefaultsToBasicAndDepthIsPresentationOnly(t *testing.T) {
	presentation := newBootstrapPresentation(tui.Capabilities{Width: 80})
	if presentation.depth != bootstrapBasic {
		t.Fatalf("default depth=%q want=%q", presentation.depth, bootstrapBasic)
	}

	snapshot := onboarding.Snapshot{State: onboarding.RuntimeConfigured, Reason: "model_incomplete"}
	before := snapshot
	presentation.setDepth(bootstrapAdvanced)
	if !reflect.DeepEqual(snapshot, before) {
		t.Fatalf("presentation depth changed canonical snapshot: before=%+v after=%+v", before, snapshot)
	}
	presentation.setDepth(bootstrapBasic)
	if presentation.depth != bootstrapBasic {
		t.Fatalf("depth=%q want=%q", presentation.depth, bootstrapBasic)
	}
}

func TestBootstrapDecisionExpandsExactDetailsBeforeApproval(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	presentation := newBootstrapPresentation(tui.Capabilities{Width: 80})
	input := newTerminalInput(strings.NewReader("advanced\nyes\n"))

	approved, err := presentation.approve(cmd, input, bootstrapDecision{
		Title:          "Bind exact local model",
		Recommendation: "Use the verified installed candidate.",
		Consequence:    "Writes the digest-bound model route; no cloud fallback is enabled.",
		Details:        "model=qwen digest=sha256:abcdef endpoint=http://127.0.0.1:11434",
	})
	if err != nil || !approved {
		t.Fatalf("approved=%t error=%v", approved, err)
	}
	if presentation.depth != bootstrapAdvanced {
		t.Fatalf("depth=%q want=%q", presentation.depth, bootstrapAdvanced)
	}
	text := output.String()
	for _, expected := range []string{
		"DECISION / Bind exact local model",
		"RECOMMENDATION",
		"CONSEQUENCE",
		"DETAILS / authoritative Aegis evidence",
		"sha256:abcdef",
		"Approve? [Y/n/details/basic/advanced]",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("output missing %q: %s", expected, text)
		}
	}
}

func TestBootstrapDecisionBasicAndAdvancedApprovalHaveStateParity(t *testing.T) {
	decision := bootstrapDecision{
		Title:          "Bind exact local model",
		Recommendation: "Use the verified installed candidate.",
		Consequence:    "Writes the digest-bound model route.",
		Details:        "model=qwen digest=sha256:abcdef endpoint=http://127.0.0.1:11434",
	}
	run := func(inputText string) (bool, onboarding.Snapshot) {
		var output bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		cmd.SetOut(&output)
		snapshot := onboarding.Snapshot{State: onboarding.RuntimeConfigured, Reason: "model_incomplete"}
		approved, err := newBootstrapPresentation(tui.Capabilities{Width: 80}).approve(cmd, newTerminalInput(strings.NewReader(inputText)), decision)
		if err != nil {
			t.Fatal(err)
		}
		return approved, snapshot
	}

	basicApproved, basicSnapshot := run("yes\n")
	advancedApproved, advancedSnapshot := run("advanced\nyes\n")
	if basicApproved != advancedApproved || !basicApproved {
		t.Fatalf("approval mismatch: basic=%t advanced=%t", basicApproved, advancedApproved)
	}
	if !reflect.DeepEqual(basicSnapshot, advancedSnapshot) {
		t.Fatalf("presentation depth changed artifact-derived state: basic=%+v advanced=%+v", basicSnapshot, advancedSnapshot)
	}
}

func TestBootstrapDecisionDeclineThenResumeUsesSameDecisionAndState(t *testing.T) {
	decision := bootstrapDecision{
		Title:          "Run end-to-end certification",
		Recommendation: "Run only when the workstation can sustain the workload.",
		Consequence:    "Declining writes no certification; rerunning resumes from verified artifacts.",
		Details:        "path=Hermes Agent -> authenticated Aegis proxy -> Ollama",
	}
	snapshot := onboarding.Snapshot{State: onboarding.ModelPresent, Reason: "manager_certification_absent"}
	before := snapshot

	var declinedOutput bytes.Buffer
	declined := &cobra.Command{}
	declined.SetContext(context.Background())
	declined.SetOut(&declinedOutput)
	approved, err := newBootstrapPresentation(tui.Capabilities{Width: 80}).approve(declined, newTerminalInput(strings.NewReader("advanced\nno\n")), decision)
	if err != nil || approved {
		t.Fatalf("decline approved=%t error=%v", approved, err)
	}
	if !reflect.DeepEqual(snapshot, before) {
		t.Fatalf("decline changed artifact state: before=%+v after=%+v", before, snapshot)
	}

	var resumedOutput bytes.Buffer
	resumed := &cobra.Command{}
	resumed.SetContext(context.Background())
	resumed.SetOut(&resumedOutput)
	approved, err = newBootstrapPresentation(tui.Capabilities{Width: 80}).approve(resumed, newTerminalInput(strings.NewReader("yes\n")), decision)
	if err != nil || !approved {
		t.Fatalf("resume approved=%t error=%v", approved, err)
	}
	if !reflect.DeepEqual(snapshot, before) {
		t.Fatalf("resume presentation changed artifact state: before=%+v after=%+v", before, snapshot)
	}
	for _, expected := range []string{"workstation can sustain", "Declining writes no certification", "Approve? [Y/n/details/basic/advanced]"} {
		if !strings.Contains(resumedOutput.String(), expected) {
			t.Fatalf("resume output missing %q: %s", expected, resumedOutput.String())
		}
	}
}

func TestBootstrapDecisionDeclineAndEOFNeverImplyApproval(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "decline", input: "no\n"},
		{name: "EOF", input: ""},
		{name: "invalid", input: "accept-it\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			cmd := &cobra.Command{}
			cmd.SetContext(context.Background())
			cmd.SetOut(&output)
			approved, err := newBootstrapPresentation(tui.Capabilities{Width: 80}).approve(cmd, newTerminalInput(strings.NewReader(test.input)), bootstrapDecision{Title: "Mutate", Recommendation: "Recommended", Consequence: "Writes state", Details: "exact evidence"})
			if err != nil {
				t.Fatal(err)
			}
			if approved {
				t.Fatalf("input %q implied approval", test.input)
			}
		})
	}
}

func TestBootstrapDecisionCanKeepSafeDeclineDefault(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	approved, err := newBootstrapPresentation(tui.Capabilities{Width: 80}).approve(cmd, newTerminalInput(strings.NewReader("\n")), bootstrapDecision{
		Title:          "Start manager gateway",
		Recommendation: "Start only when interactive manager access is intended now.",
		Consequence:    "Starts the authenticated local manager surface.",
		Details:        "route=local-only; no fallback=true",
		DefaultDecline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Fatal("empty answer implied gateway activation")
	}
	if !strings.Contains(output.String(), "Approve? [y/N/details/basic/advanced]") {
		t.Fatalf("safe default not visible: %s", output.String())
	}
}

func TestBootstrapPresentationWrapsHierarchyAtSupportedWidths(t *testing.T) {
	for _, width := range []int{80, 100, 120} {
		var output bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetContext(context.Background())
		cmd.SetOut(&output)
		presentation := newBootstrapPresentation(tui.Capabilities{Width: width})
		_, err := presentation.approve(cmd, newTerminalInput(strings.NewReader("advanced\nno\n")), bootstrapDecision{
			Title:          "Run certification",
			Recommendation: "Run now when the workstation can sustain the exact local model workload.",
			Consequence:    "May use substantial CPU, GPU, RAM, and time; declining writes no certification and rerunning resumes from verified artifacts.",
			Details:        "Hermes Agent -> authenticated Aegis proxy -> Ollama; exact-path=" + strings.Repeat("a", 2*width) + "; every named corpus case must pass.",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
			if len([]rune(line)) > width {
				t.Fatalf("width %d produced %d-column line %q", width, len([]rune(line)), line)
			}
		}
	}
}

func TestBootstrapCustodyChoiceUsesSharedPresentationAndPreservesExactAdvancedChoices(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	presentation := newBootstrapPresentation(tui.Capabilities{Width: 80})
	custody, err := presentation.chooseCustody(cmd, newTerminalInput(strings.NewReader("advanced\n1\n")), bootstrapDecision{
		Title:          "Choose credential authority custody",
		Recommendation: "Use encrypted local custody.",
		Consequence:    "Declining performs no mutation.",
		Details:        "systemd is externally delivered; host-file is weaker",
	})
	if err != nil || custody != "passphrase-file" {
		t.Fatalf("custody=%q error=%v", custody, err)
	}
	for _, expected := range []string{"RECOMMENDATION", "CONSEQUENCE", "DETAILS / authoritative Aegis evidence", "EXACT CHOICES", "passphrase-encrypted local key", "systemd service credential", "plaintext host file", "exit without mutation"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("advanced custody output missing %q: %s", expected, output.String())
		}
	}
}

func TestBootstrapDecisionLimitedTerminalKeepsSafeDeclineAndDetailsRoute(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	presentation := newBootstrapPresentation(tui.Capabilities{Width: 20})
	approved, err := presentation.approve(cmd, newTerminalInput(strings.NewReader("details\nno\n")), bootstrapDecision{
		Title:          "Certify",
		Recommendation: "Run exact local certification when resources are available.",
		Consequence:    "Declining writes no certification and resume remains artifact-derived.",
		Details:        "digest-bound exact route",
	})
	if err != nil || approved {
		t.Fatalf("approved=%t error=%v", approved, err)
	}
	if !strings.Contains(output.String(), "Approve? [Y/n/details]:") || !strings.Contains(output.String(), "digest-bound exact route") {
		t.Fatalf("limited presentation lost decline/details route: %s", output.String())
	}
	for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
		if len([]rune(line)) > 40 {
			t.Fatalf("limited terminal produced %d-column line %q", len([]rune(line)), line)
		}
	}
}
