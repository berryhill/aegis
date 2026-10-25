package implementation

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

// Exact Git and candidate-object producers must not bypass the owning native port.
func TestContributionNativeProducerPortDenial(t *testing.T) {
	denied := errors.New("fresh admission denied")
	calls := 0
	e := &ManagedContribution{Native: func(ctx context.Context, cmd *exec.Cmd) error {
		calls++
		if cmd.Process != nil {
			t.Fatal("command started before admission")
		}
		return denied
	}}
	cmd := exec.CommandContext(context.Background(), "/usr/bin/true")
	_, err := e.commandOutput(context.Background(), cmd)
	if !errors.Is(err, denied) || calls != 1 || cmd.Process != nil {
		t.Fatalf("native admission bypassed: calls=%d err=%v", calls, err)
	}
}

func TestContributionNativeProducerBoundedStreams(t *testing.T) {
	calls := 0
	e := &ManagedContribution{Native: func(ctx context.Context, cmd *exec.Cmd) error { calls++; return cmd.Run() }}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("Git unavailable")
	}
	b, err := e.commandOutput(context.Background(), exec.CommandContext(context.Background(), git, "--version"))
	if err != nil || len(b) == 0 || calls != 1 {
		t.Fatalf("real Git producer: %s %v calls=%d", b, err, calls)
	}
}
