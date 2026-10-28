package app

import "context"

// A terminal lock serializes only one session. Its lifetime includes its holder
// and waiters, never the historical session inventory. No authority is cached.
type sessionTerminalLock struct {
	token chan struct{}
	users int
}

func (s *Service) lockSessionTerminal(ctx context.Context, id string) (func(), error) {
	if ctx == nil || id == "" {
		return nil, ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.sessionTerminalMu.Lock()
	if s.sessionTerminalLocks == nil {
		s.sessionTerminalLocks = make(map[string]*sessionTerminalLock)
	}
	lock := s.sessionTerminalLocks[id]
	if lock == nil {
		lock = &sessionTerminalLock{token: make(chan struct{}, 1)}
		s.sessionTerminalLocks[id] = lock
	}
	lock.users++
	s.sessionTerminalMu.Unlock()

	drop := func() {
		s.sessionTerminalMu.Lock()
		defer s.sessionTerminalMu.Unlock()
		lock.users--
		if lock.users == 0 {
			delete(s.sessionTerminalLocks, id)
		}
	}
	select {
	case lock.token <- struct{}{}:
		// Cancellation can race a newly available token. Deny before any
		// terminal read/write even if select chose the token in that race.
		if err := ctx.Err(); err != nil {
			<-lock.token
			drop()
			return nil, err
		}
		return func() {
			<-lock.token
			drop()
		}, nil
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	}
}
