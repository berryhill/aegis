package orchestration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestDoerImplementationInputCarriesExactAcceptanceWithoutHostPath(t *testing.T) {
	text := "hello"
	contract := loop.DoerContract{Task: "Create result.txt", Workspace: "/private/operator/workspace", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 2}
	wire, err := json.Marshal(newDoerImplementationInput(contract, "repair newline"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Task          string   `json:"task"`
		Feedback      string   `json:"feedback"`
		WritableFiles []string `json:"writable_files"`
		VerifyFile    string   `json:"verify_file"`
		ExpectedText  *string  `json:"expected_text"`
		ExactBytes    bool     `json:"exact_bytes"`
		MaxAttempts   uint16   `json:"max_attempts"`
	}
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatal(err)
	}
	if got.Task != contract.Task || got.Feedback != "repair newline" || len(got.WritableFiles) != 1 || got.WritableFiles[0] != "result.txt" || got.VerifyFile != "result.txt" || got.ExpectedText == nil || *got.ExpectedText != "hello" || !got.ExactBytes || got.MaxAttempts != 2 {
		t.Fatalf("lost exact acceptance: %s", wire)
	}
	if strings.Contains(string(wire), contract.Workspace) {
		t.Fatal("host workspace disclosed to tool-free model")
	}
	contract.ExpectedText = nil
	contract.ExactBytes = false
	wire, err = json.Marshal(newDoerImplementationInput(contract, ""))
	if err != nil || strings.Contains(string(wire), "expected_text") {
		t.Fatalf("presence check became empty-text assertion: %s %v", wire, err)
	}
	empty := ""
	contract.ExpectedText = &empty
	wire, err = json.Marshal(newDoerImplementationInput(contract, ""))
	if err != nil || !strings.Contains(string(wire), `"expected_text":""`) {
		t.Fatalf("explicit empty assertion lost: %s %v", wire, err)
	}
}
