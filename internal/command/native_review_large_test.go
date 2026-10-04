package command

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestLargeNativeReviewFixture(t *testing.T) {
	mode := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "--aegis-review-fixture=") {
			mode = strings.TrimPrefix(arg, "--aegis-review-fixture=")
		}
	}
	if mode == "" {
		return
	}
	data, _ := io.ReadAll(os.Stdin)
	if len(data) < 4096 {
		os.Exit(9)
	}
	args := strings.Join(os.Args, " ")
	if !strings.Contains(args, "--checkbox=I reviewed and approve only this exact scope") || !strings.Contains(args, "--ok-label=Approve exact scope") {
		os.Exit(8)
	}
	switch mode {
	case "deny":
		os.Stdout.WriteString("Deny\n")
	case "cancel":
		os.Exit(1)
	case "unknown":
		os.Stdout.WriteString("arbitrary reply")
	}
	os.Exit(0)
}

func TestLargeNativeReviewDoesNotTruncateScope(t *testing.T) {
	data := strings.Repeat("bounded task data\n", 600)
	if confirmationDataSafe(data) || !nativeReviewDataSafe(data) {
		t.Fatal("large native review bounds are not distinct from pinentry line bounds")
	}
	for _, scenario := range []struct{ mode, want string }{{"approve", "confirmed"}, {"deny", "denied"}, {"cancel", "cancelled"}, {"unknown", "malformed_protocol"}} {
		t.Run(scenario.mode, func(t *testing.T) {
			s := newNativeConfirmation()
			s.timeout = time.Second
			s.command = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
				cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestLargeNativeReviewFixture$", "--", "--aegis-review-fixture=" + scenario.mode}, args...)...)
				return cmd
			}
			result := s.largeReview(context.Background(), "/unused", data)
			if result != scenario.want {
				t.Fatalf("native decision = %s, expected %s", result, scenario.want)
			}
		})
	}
}
