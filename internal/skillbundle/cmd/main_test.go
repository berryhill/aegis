package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/skillbundle"
)

func TestEvaluateCLIRejectsMalformedFixturesWithoutSuccessOutput(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "skills"), os.DirFS("../../../skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "evaluations.json"), []byte(`{"schema_version":1,"cases":[],"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original }()
	err = run([]string{"evaluate", root})
	var denial *skillbundle.Denial
	if !errors.As(err, &denial) || denial.Code != "evaluations_malformed" {
		t.Fatalf("expected malformed fixture denial, got %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("denied fixture emitted a success result: %s", data)
	}
}

func TestEvaluateCLIReportsStructuralValidation(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original }()
	if err := run([]string{"evaluate", "../../.."}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid result %q: %v", data, err)
	}
	if result["status"] != "valid" || result["evidence_class"] != "structural_fixture_validation" || result["behavioral_execution"] != "not_run" {
		t.Fatalf("CLI lost evidence scope: %s", data)
	}
	if _, exists := result["passed"]; exists {
		t.Fatalf("CLI implies behavioral passes: %s", data)
	}
}
