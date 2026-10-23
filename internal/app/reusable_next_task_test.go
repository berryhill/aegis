package app

import (
	"github.com/berryhill/aegis/internal/loop"
	"testing"
)

func TestReusableNextTaskBudgetBoundary(t *testing.T) {
	text := "hello"
	previous := loop.DoerContract{Task: "Write hello", Workspace: "/workspace", WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, MaxAttempts: 2}
	for _, tc := range []struct {
		name    string
		change  func(*loop.DoerContract)
		linked  bool
		allowed bool
	}{
		{"same task fresh key", func(*loop.DoerContract) {}, false, false},
		{"same task linked remaining budget", func(*loop.DoerContract) {}, true, true},
		{"new workspace new task", func(c *loop.DoerContract) { c.Workspace = "/other-workspace" }, false, true},
		{"changed task cannot claim old continuation", func(c *loop.DoerContract) { c.Task = "Different task" }, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := previous
			tc.change(&next)
			if got := reusableNextTaskAllowed(previous, next, tc.linked); got != tc.allowed {
				t.Fatalf("allowed=%v want=%v", got, tc.allowed)
			}
		})
	}
}
