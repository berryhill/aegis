package manager

import (
	"context"
	"errors"
	"sync"
)

type LifecycleState string

const (
	LifecycleCreated      LifecycleState = "created"
	LifecyclePreflighting LifecycleState = "preflighting"
	LifecycleStarting     LifecycleState = "starting"
	LifecycleActive       LifecycleState = "active"
	LifecycleDegraded     LifecycleState = "degraded"
	LifecycleClosing      LifecycleState = "closing"
	LifecycleCleaning     LifecycleState = "cleaning"
	LifecycleClosed       LifecycleState = "closed"
	LifecycleFailed       LifecycleState = "failed"
)

const (
	EndUserExit       = "user_exit"
	EndTerminalEOF    = "terminal_eof"
	EndInterrupt      = "interrupt"
	EndTermination    = "termination"
	EndSessionExpired = "session_expired"
	EndSessionRevoked = "session_revoked"
	EndRuntimeFailed  = "runtime_failed"
	EndStartupFailed  = "startup_failed"
	EndCleanupFailed  = "cleanup_failed"
)

var (
	ErrInterrupt      = errors.New(EndInterrupt)
	ErrTermination    = errors.New(EndTermination)
	ErrSessionExpired = errors.New(EndSessionExpired)
	ErrSessionRevoked = errors.New(EndSessionRevoked)
	ErrRuntimeFailed  = errors.New(EndRuntimeFailed)
)

var lifecycleOrder = map[LifecycleState]int{
	LifecycleCreated: 0, LifecyclePreflighting: 1, LifecycleStarting: 2,
	LifecycleActive: 3, LifecycleDegraded: 3, LifecycleClosing: 4, LifecycleCleaning: 5,
	LifecycleClosed: 6, LifecycleFailed: 6,
}

type Lifecycle struct {
	mu     sync.Mutex
	state  LifecycleState
	reason string
}

func NewLifecycle() *Lifecycle { return &Lifecycle{state: LifecycleCreated} }

func (l *Lifecycle) Advance(next LifecycleState) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	current, currentOK := lifecycleOrder[l.state]
	target, targetOK := lifecycleOrder[next]
	if !currentOK || !targetOK || target < current || (current >= lifecycleOrder[LifecycleClosing] && target < lifecycleOrder[LifecycleClosing]) || (current == lifecycleOrder[LifecycleClosed] && next != l.state) {
		return errors.New("invalid manager lifecycle transition")
	}
	l.state = next
	return nil
}

func (l *Lifecycle) RequestClose(reason string) error {
	if !ValidEndReason(reason) {
		return errors.New("invalid manager end reason")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reason == "" {
		l.reason = reason
	}
	if lifecycleOrder[l.state] < lifecycleOrder[LifecycleClosing] {
		l.state = LifecycleClosing
	}
	return nil
}

func (l *Lifecycle) Snapshot() (LifecycleState, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state, l.reason
}

func ValidEndReason(reason string) bool {
	switch reason {
	case EndUserExit, EndTerminalEOF, EndInterrupt, EndTermination, EndSessionExpired, EndSessionRevoked, EndRuntimeFailed, EndStartupFailed, EndCleanupFailed:
		return true
	default:
		return false
	}
}

func EndReasonFromContext(ctx context.Context) string {
	cause := context.Cause(ctx)
	switch {
	case errors.Is(cause, ErrInterrupt):
		return EndInterrupt
	case errors.Is(cause, ErrTermination):
		return EndTermination
	case errors.Is(cause, ErrSessionExpired):
		return EndSessionExpired
	case errors.Is(cause, ErrSessionRevoked):
		return EndSessionRevoked
	case errors.Is(cause, ErrRuntimeFailed):
		return EndRuntimeFailed
	default:
		return EndRuntimeFailed
	}
}
