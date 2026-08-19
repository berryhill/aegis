package command

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestRootDoesNotExposeLegacyPlumbingCommand(t *testing.T) {
	root := NewRoot(Dependencies{In: nil, Out: io.Discard, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	for _, command := range root.Commands() {
		if command.Name() == "plumbing" {
			t.Fatal("legacy plumbing command remains on the production surface")
		}
	}
}

func TestRootExposesFleetProductResources(t *testing.T) {
	root := NewRoot(Dependencies{In: nil, Out: io.Discard, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	for _, path := range [][]string{
		{"agents", "register"}, {"agents", "list"}, {"agents", "show"}, {"agents", "history"},
		{"agents", "enable"}, {"agents", "disable"}, {"agents", "retire"},
		{"loops", "list"}, {"loops", "publish"}, {"loops", "show"}, {"loops", "activate"}, {"loops", "retire"},
		{"graphs", "list"}, {"graphs", "publish"}, {"graphs", "show"}, {"graphs", "submit"},
		{"queue", "list"}, {"queue", "show"}, {"queue", "process"}, {"queue", "retry"},
		{"queue", "cancel"}, {"queue", "expire"}, {"queue", "exhaust"},
	} {
		command, _, err := root.Find(path)
		if err != nil || command == nil || command.Name() != path[len(path)-1] {
			t.Fatalf("missing constructor-built fleet CLI resource %v: command=%v err=%v", path, command, err)
		}
	}
}

func TestFleetCLIInputRejectsUnknownFieldsAndTrailingValues(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field":  `{"fixture":{},"identity":{},"subject":"model-selected"}`,
		"trailing value": `{"fixture":{},"identity":{}} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "request.json")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			var value struct {
				Fixture  map[string]any `json:"fixture"`
				Identity map[string]any `json:"identity"`
			}
			if err := decodeJSONFile(path, &value); err == nil {
				t.Fatal("ambiguous fleet CLI input was accepted")
			}
		})
	}
}
