package command

import (
	"bytes"
	"context"
	"strings"
	"testing"

	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/spf13/cobra"
)

func TestSelectInstalledCandidateSkipsOneItemMenu(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	input := newTerminalInput(strings.NewReader("next answer\n"))
	installed := []managerdomain.InstalledCandidate{{Candidate: managerdomain.Candidate{ID: "only-candidate"}}}

	renderInstalledCandidates(cmd, installed)
	selected, ok, err := selectInstalledCandidate(cmd, input, installed)
	if err != nil || !ok || selected.Candidate.ID != "only-candidate" {
		t.Fatalf("selected=%+v ok=%v error=%v", selected, ok, err)
	}
	if strings.Contains(output.String(), "[1]") || strings.Contains(output.String(), "Select") || !strings.Contains(output.String(), "selected automatically: only-candidate") {
		t.Fatalf("unexpected one-candidate output: %q", output.String())
	}
	next, eof, err := readBootstrapLine(cmd, input, 32)
	if err != nil || eof || next != "next answer" {
		t.Fatalf("automatic selection consumed next input: next=%q eof=%v error=%v", next, eof, err)
	}
}

func TestSelectInstalledCandidatePromptsForMultipleItems(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&output)
	input := newTerminalInput(strings.NewReader("2\n"))
	installed := []managerdomain.InstalledCandidate{
		{Candidate: managerdomain.Candidate{ID: "first"}},
		{Candidate: managerdomain.Candidate{ID: "second"}},
	}

	renderInstalledCandidates(cmd, installed)
	selected, ok, err := selectInstalledCandidate(cmd, input, installed)
	if err != nil || !ok || selected.Candidate.ID != "second" {
		t.Fatalf("selected=%+v ok=%v error=%v", selected, ok, err)
	}
	if !strings.Contains(output.String(), "[1] first") || !strings.Contains(output.String(), "[2] second") || !strings.Contains(output.String(), "Select one installed candidate number (no default):") {
		t.Fatalf("multiple-candidate prompt missing: %q", output.String())
	}
}
