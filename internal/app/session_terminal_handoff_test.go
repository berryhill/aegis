package app

import (
	"context"
	"testing"
	"time"
)

func waitTerminalContenders(t *testing.T, s *Service, id string, count int) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		s.sessionTerminalMu.Lock()
		lock := s.sessionTerminalLocks[id]
		ready := lock != nil && lock.users == count
		s.sessionTerminalMu.Unlock()
		if ready {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("terminal contenders never reached lock admission")
		}
	}
}

func TestSessionTerminalLockSuccessfulHandoff(t *testing.T) {
	s := new(Service)
	holder, err := s.lockSessionTerminal(context.Background(), "handoff")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if holder != nil {
			holder()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		unlock, err := s.lockSessionTerminal(ctx, "handoff")
		if unlock != nil {
			unlock()
		}
		result <- err
	}()
	waitTerminalContenders(t, s, "handoff", 2)
	select {
	case <-result:
		t.Fatal("waiter entered while original holder owned token")
	default:
	}
	holder()
	holder = nil
	select {
	case err := <-result:
		if err != nil {
			t.Fatal("successful handoff denied", err)
		}
	case <-ctx.Done():
		t.Fatal("released token was not handed off")
	}
	s.sessionTerminalMu.Lock()
	remaining := len(s.sessionTerminalLocks)
	s.sessionTerminalMu.Unlock()
	if remaining != 0 {
		t.Fatal("handoff retained terminal lock entry")
	}
	if unlock, err := s.lockSessionTerminal(nil, "valid"); err == nil || unlock != nil {
		t.Fatal("nil context accepted")
	}
	if unlock, err := s.lockSessionTerminal(context.Background(), ""); err == nil || unlock != nil {
		t.Fatal("empty session accepted")
	}
}
