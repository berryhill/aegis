package app

import (
	"context"
	"github.com/berryhill/aegis/internal/orchestration"
	"sync"
)

// This private slot accepts one verifier-prepared canonical write, not evidence
// or an acceptance callback. It exists only for a synchronous opted-in frame.
type contributionPreparedTerminal struct {
	contract, candidate, session string
	commit                       func(context.Context) error
}
type contributionTerminalSlot struct {
	mu                    sync.Mutex
	open, valid, consumed bool
	pending               *contributionPreparedTerminal
	proof                 *contributionCustodyProof
}

func (s *contributionTerminalSlot) register(p *contributionCustodyProof, prepared *contributionPreparedTerminal) error {
	if s == nil || p == nil || prepared == nil || prepared.commit == nil {
		return ErrDenied
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.valid || !s.open || s.consumed || s.pending != nil || !p.live.Load() || p.terminal != s || p.contract != prepared.contract || p.candidate != prepared.candidate || p.session != prepared.session {
		return ErrDenied
	}
	s.pending = prepared
	s.proof = p
	return nil
}
func (s *contributionTerminalSlot) closeAction() { s.mu.Lock(); defer s.mu.Unlock(); s.open = false }
func (s *contributionTerminalSlot) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.open = false
	s.valid = false
	s.pending = nil
}
func (s *contributionTerminalSlot) registered() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending != nil && s.valid
}
func (s *contributionTerminalSlot) consume(ctx context.Context, c orchestration.ContributionCandidate) error {
	s.mu.Lock()
	if !s.valid || s.open || s.consumed || ctx.Err() != nil || (s.proof != nil && s.proof.live.Load()) {
		s.mu.Unlock()
		return ErrDenied
	}
	s.consumed = true
	prepared := s.pending
	s.pending = nil
	s.mu.Unlock()
	if prepared == nil {
		return nil
	}
	if prepared.contract != c.Contract || prepared.candidate != c.Digest {
		return ErrDenied
	}
	return prepared.commit(ctx)
}
