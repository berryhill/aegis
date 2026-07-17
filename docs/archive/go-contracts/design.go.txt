package specs

import (
	"context"
	"time"
)

type DesignSessionID string

type DesignLaunchSpec struct {
	Principal         AuthenticatedSubject
	Runtime           RuntimeDescriptor
	Isolation         IsolationLevel
	AllowedTools      []ToolID
	ReadOnly          bool
	Provisioning      bool
	AmbientMemory     bool
	AmbientPlugins    bool
	AmbientMCP        bool
	PersistentProfile bool
}

type DesignSession struct {
	ID             DesignSessionID
	Principal      PrincipalID
	RuntimeSession RuntimeSession
	StartedAt      time.Time
	ExpiresAt      time.Time
	ModeLabel      string
}

type DesignInput struct {
	SessionID DesignSessionID
	Message   string
}

type DesignOutput struct {
	Message      string
	CharterDraft *Charter
	Warnings     []string
}

// Designer is a proposal-only surface. Start must reject unauthenticated or
// non-principal callers, and BuildCharter must not provision runtime artifacts.
type Designer interface {
	Start(context.Context, AuthenticatedSubject, RuntimeSelection) (DesignSession, error)
	Continue(context.Context, DesignInput) (DesignOutput, error)
	BuildCharter(context.Context, DesignSessionID) (Charter, error)
	Close(context.Context, DesignSessionID) error
}
