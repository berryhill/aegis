//go:build linux

package hermes

import (
	"context"
	"github.com/berryhill/aegis/internal/contribution"
	"os/exec"
	"testing"
)

func TestContributionNativeExactWaitProof(t *testing.T) {
	for _, program := range []string{"true", "false"} {
		t.Run(program, func(t *testing.T) {
			cmd := exec.Command(program)
			b := contribution.NativeBinding{IntentID: "one", CommandDigest: NativeCommandDigest(cmd)}
			reserved, finished := false, false
			err := RunContributionNative(context.Background(), b, cmd, func(context.Context) error { reserved = true; return nil }, func(_ context.Context, p NativeCompletion) error {
				if !reserved || !p.Joined() || p.Binding() != b || cmd.ProcessState == nil {
					t.Fatal("not exact Wait proof")
				}
				finished = true
				return nil
			})
			if !finished || (program == "true" && err != nil) || (program == "false" && err == nil) {
				t.Fatalf("completion/success confused: %v", err)
			}
		})
	}
	cmd := exec.Command("true")
	b := contribution.NativeBinding{CommandDigest: "wrong"}
	if RunContributionNative(context.Background(), b, cmd, func(context.Context) error { t.Fatal("reserved substituted command"); return nil }, func(context.Context, NativeCompletion) error { return nil }) == nil {
		t.Fatal("substitution")
	}
	if (NativeCompletion{}).Joined() {
		t.Fatal("caller minted completion")
	}
}
