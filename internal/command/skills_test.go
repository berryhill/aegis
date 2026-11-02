package command

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSkillsPackagedSurface(t *testing.T) {
	for _, verb := range []string{"install", "inventory", "update", "rollback"} {
		t.Run(verb, func(t *testing.T) {
			var out bytes.Buffer
			root := NewRoot(Dependencies{Out: &out, Err: &out, Version: "test"})
			root.SetArgs([]string{"skills", verb})
			err := root.ExecuteContext(context.Background())
			if err == nil || strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("expected argument validation, not unknown command: %v", err)
			}
			root = NewRoot(Dependencies{Out: &out, Err: &out, Version: "test"})
			root.SetArgs([]string{"skills", verb, "--help"})
			if err := root.ExecuteContext(context.Background()); err != nil {
				t.Fatalf("packaged command unavailable: %v", err)
			}
			if !strings.Contains(out.String(), "aegis skills "+verb) {
				t.Fatalf("missing command help: %s", &out)
			}
			child, _, err := root.Find([]string{"skills", verb})
			if err != nil || commandNeedsFleet(child) || commandNeedsCredentials(child) {
				t.Fatalf("filesystem utility classified as a store operation: %v", err)
			}
		})
	}
}
