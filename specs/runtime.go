package specs

import (
	"context"
	"time"
)

type RuntimeSelectionRequest struct {
	ExplicitAdapter   RuntimeAdapterID
	ExplicitRuntime   RuntimeID
	CharterRuntime    RuntimeConstraint
	ConfiguredDefault RuntimeConstraint
}

type RuntimeSelection struct {
	Descriptor RuntimeDescriptor
	Source     string
	Visible    bool
}

type RuntimeRegistry interface {
	List(context.Context) ([]RuntimeDescriptor, error)
	Resolve(context.Context, RuntimeSelectionRequest) (RuntimeSelection, error)
	Adapter(RuntimeAdapterID) (RuntimeAdapter, error)
}

type IsolationLevel string

const (
	IsolationDisposableHome IsolationLevel = "disposable_home"
	IsolationProcess        IsolationLevel = "process"
	IsolationSandbox        IsolationLevel = "sandbox"
)

type RuntimeLaunchSpec struct {
	Runtime           RuntimeDescriptor
	AgentID           AgentID
	StanzaID          StanzaID
	CharterDigest     Digest
	Capabilities      EffectiveCapabilities
	Scopes            ScopeSet
	Isolation         IsolationLevel
	StateDirectory    string
	PersistentProfile string
	DisableAmbientMCP bool
	DisablePlugins    bool
	ExpiresAt         time.Time
}

type RuntimeSession struct {
	ID         RuntimeSessionID
	Descriptor RuntimeDescriptor
	StartedAt  time.Time
	StatePath  string
	Effective  RuntimeLaunchSpec
}

type RuntimeInspection struct {
	Session RuntimeSession
	Tools   []ToolID
	Healthy bool
	Details map[string]string
}

// RuntimeAdapter keeps the concrete runtime visible. Launch must start a clean
// execution context matching the supplied spec; it must not infer additional
// authority from ambient profiles, prompts, plugins, or MCP servers.
type RuntimeAdapter interface {
	Discover(context.Context) (RuntimeDescriptor, error)
	Validate(context.Context, Charter) error
	StartDesign(context.Context, DesignLaunchSpec) (RuntimeSession, error)
	Launch(context.Context, RuntimeLaunchSpec) (RuntimeSession, error)
	Inspect(context.Context, RuntimeSessionID) (RuntimeInspection, error)
	Terminate(context.Context, RuntimeSessionID, string) error
}
