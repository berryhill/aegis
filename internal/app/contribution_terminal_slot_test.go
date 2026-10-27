package app

import (
	"context"
	"github.com/berryhill/aegis/internal/orchestration"
	"sync"
	"testing"
)

// These tests qualify private scheduling/lifetime only, not verifier authority.
// The actual canonical-store proof is exercised by the full HTTP journey.
func TestContributionTerminalSlotLifecycle(t *testing.T) {
	c := orchestration.ContributionCandidate{Contract: "contract", Digest: "candidate"}
	slot := &contributionTerminalSlot{open: true, valid: true}
	proof := &contributionCustodyProof{contract: c.Contract, candidate: c.Digest, session: "session", terminal: slot}
	proof.live.Store(true)
	calls := 0
	prepared := &contributionPreparedTerminal{contract: c.Contract, candidate: c.Digest, session: proof.session, commit: func(context.Context) error { calls++; return nil }}
	if slot.register(nil, prepared) == nil {
		t.Fatal("no private frame accepted")
	}
	if slot.register(proof, prepared) != nil {
		t.Fatal("live frame registration failed")
	}
	if slot.register(proof, prepared) == nil {
		t.Fatal("duplicate registration accepted")
	}
	if slot.consume(context.Background(), c) == nil {
		t.Fatal("commit while action open")
	}
	slot.closeAction()
	if slot.consume(context.Background(), c) == nil {
		t.Fatal("general effect authority still live at commit")
	}
	proof.live.Store(false)
	if slot.register(proof, prepared) == nil {
		t.Fatal("escaped effect frame reusable")
	}
	if slot.consume(context.Background(), c) != nil || calls != 1 {
		t.Fatal("closed synchronous metadata commit failed")
	}
	if slot.consume(context.Background(), c) == nil || calls != 1 {
		t.Fatal("terminal replay")
	}
	slot.invalidate()
	if slot.register(proof, prepared) == nil {
		t.Fatal("outliving slot reusable")
	}
}

func TestContributionTerminalSlotConcurrentRegistration(t *testing.T) {
	slot := &contributionTerminalSlot{open: true, valid: true}
	p := &contributionCustodyProof{contract: "c", candidate: "v", session: "s", terminal: slot}
	p.live.Store(true)
	commit := &contributionPreparedTerminal{contract: "c", candidate: "v", session: "s", commit: func(context.Context) error { return nil }}
	var wg sync.WaitGroup
	successes := make(chan bool, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); successes <- slot.register(p, commit) == nil }()
	}
	wg.Wait()
	close(successes)
	n := 0
	for ok := range successes {
		if ok {
			n++
		}
	}
	if n != 1 {
		t.Fatal("registration winners", n)
	}
	slot.invalidate()
}

func TestContributionTerminalSlotAbortedBeforeCommit(t *testing.T) {
	for _, mode := range []string{"context", "candidate", "contract", "invalidated"} {
		slot := &contributionTerminalSlot{open: true, valid: true}
		p := &contributionCustodyProof{contract: "c", candidate: "v", session: "s", terminal: slot}
		p.live.Store(true)
		called := false
		if slot.register(p, &contributionPreparedTerminal{contract: "c", candidate: "v", session: "s", commit: func(context.Context) error { called = true; return nil }}) != nil {
			t.Fatal("register")
		}
		slot.closeAction()
		p.live.Store(false)
		c := orchestration.ContributionCandidate{Contract: "c", Digest: "v"}
		ctx := context.Background()
		switch mode {
		case "context":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		case "candidate":
			c.Digest = "foreign"
		case "contract":
			c.Contract = "foreign"
		case "invalidated":
			slot.invalidate()
		}
		if slot.consume(ctx, c) == nil || called {
			t.Fatal("aborted commit executed", mode)
		}
	}
}
