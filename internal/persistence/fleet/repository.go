// Package fleet defines the narrow durable boundary for fleet-control facts.
// Domain records remain owned by registry, loop, graph, and core; implementations
// must not reinterpret their canonical codecs or grant runtime authority.
package fleet

import (
	"context"
	"errors"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
)

var (
	ErrNotFound = errors.New("fleet record not found")
	ErrConflict = errors.New("fleet immutable record conflict")
	ErrCorrupt  = errors.New("fleet record is corrupt")
	ErrClosed   = errors.New("fleet repository is closed")
)

// AuditFact is metadata supplied by an authenticated application boundary.
// The repository assigns chain position, event identity, occurrence time, and
// digest in the same transaction as the consequential fleet mutation.
type AuditFact struct {
	Event core.AuditEvent
}

// Repository exposes create-only definition persistence. It deliberately has
// no generic put, update, delete, latest-substitution, or engine transaction API.
type Repository interface {
	RegisterAgent(context.Context, registry.AgentRegistration, registry.AgentRevision, AuditFact) (bool, error)
	PublishAgentRevision(context.Context, registry.AgentRevision, AuditFact) error
	GetAgentRegistration(context.Context, string) (registry.AgentRegistration, error)
	GetAgentRegistrationBySource(context.Context, registry.FleetSource) (registry.AgentRegistration, error)
	GetAgentRevision(context.Context, string, uint64) (registry.AgentRevision, error)
	LatestAgentRevision(context.Context, string) (registry.AgentRevision, error)
	ListAgentRegistrations(context.Context) ([]registry.AgentRegistration, error)

	PublishLoop(context.Context, loop.PublishRequest, AuditFact) (loop.PublicationDecision, error)
	GetLoopRevision(context.Context, string, uint64) (loop.LoopRevision, error)
	GetLoopValidation(context.Context, string, uint64, string) (loop.LoopValidationResult, error)

	PublishGraph(context.Context, graph.PublishRequest, AuditFact) (graph.PublicationDecision, error)
	GetGraphRevision(context.Context, string, uint64) (graph.GraphRevision, error)
	GetGraphValidation(context.Context, string, uint64, string) (graph.GraphValidationResult, error)
	CreateGraphRunSnapshot(context.Context, graph.GraphRunSnapshot, AuditFact) (bool, error)
	GetGraphRunSnapshot(context.Context, string) (graph.GraphRunSnapshot, error)

	AuditEvents(context.Context) ([]core.AuditEvent, error)
	Close() error
}
