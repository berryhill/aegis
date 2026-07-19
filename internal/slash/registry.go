package slash

import (
	"fmt"
	"sort"
	"strings"
)

const SchemaVersion = "aegis.manager.command.v1"

type ExecutionClass string

const (
	ImmediateRead      ExecutionClass = "immediate_read"
	BoundedJob         ExecutionClass = "bounded_job"
	LeasedSubscription ExecutionClass = "leased_subscription"
	WorkflowMutation   ExecutionClass = "workflow_mutation"
	Lifecycle          ExecutionClass = "lifecycle"
	Export             ExecutionClass = "export"
)

type Consequence string

const (
	ReadOnly     Consequence = "read_only"
	Presentation Consequence = "presentation_only"
	Mutation     Consequence = "workflow_mutation"
	SessionClose Consequence = "session_lifecycle"
)

type LifecycleState string

const (
	Startup  LifecycleState = "startup"
	Degraded LifecycleState = "degraded"
	Active   LifecycleState = "active"
	Expiring LifecycleState = "expiring"
	Revoked  LifecycleState = "revoked"
	Cleanup  LifecycleState = "cleanup"
)

type Definition struct {
	Name          string
	Aliases       []string
	Usage         string
	Grammar       string
	Class         ExecutionClass
	States        []LifecycleState
	Prerequisites []string
	Scopes        []string
	Consequence   Consequence
	Cancellable   bool
	Policy        string
	Audit         string
	ResultSchema  string
	Examples      []string
	Help          string
	Base          bool
}

type Registry struct {
	definitions []Definition
	byName      map[string]int
	aliases     map[string]string
}

func NewRegistry() (*Registry, error) {
	all := []LifecycleState{Startup, Degraded, Active, Expiring}
	active := []LifecycleState{Degraded, Active, Expiring}
	definitions := []Definition{
		{Name: "help", Usage: "/help [command|states|syntax|keyboard|aliases]", Grammar: "optional topic", Class: ImmediateRead, States: all, Consequence: ReadOnly, Policy: "manager.help", Audit: "manager.help", ResultSchema: "help.v1", Examples: []string{"/help", "/help scan"}, Help: "Registry-derived command, syntax, alias, and keyboard help.", Base: true},
		{Name: "status", Usage: "/status", Grammar: "no arguments", Class: ImmediateRead, States: all, Consequence: ReadOnly, Policy: "manager.status", Audit: "manager.status", ResultSchema: "status.v1", Help: "Current authoritative lifecycle, readiness, freshness, and operation state.", Base: true},
		{Name: "context", Usage: "/context", Grammar: "no arguments", Class: ImmediateRead, States: active, Consequence: ReadOnly, Policy: "manager.context", Audit: "manager.context", ResultSchema: "context.v1", Help: "Authenticated subject, single stanza, mandate, runtime, policy, and isolation context.", Base: true},
		{Name: "authority", Usage: "/authority [operation]", Grammar: "optional canonical operation", Class: ImmediateRead, States: active, Consequence: ReadOnly, Policy: "manager.authority", Audit: "manager.authority", ResultSchema: "authority.v1", Help: "Read-only explanation of current operation decisions; never grants or switches authority.", Base: true},
		{Name: "limits", Usage: "/limits", Grammar: "no arguments", Class: ImmediateRead, States: all, Consequence: ReadOnly, Policy: "manager.limits", Audit: "manager.limits", ResultSchema: "limits.v1", Help: "Known blind spots, missing sources, bounds, retention, and isolation limitations.", Base: true},
		{Name: "scan", Aliases: []string{"scan-secrets", "scan-processes", "scan-network", "scan-files"}, Usage: "/scan [core|status <id>|list]", Grammar: "closed scan form", Class: BoundedJob, States: active, Prerequisites: []string{"authenticated manager context"}, Scopes: []string{"manager-session"}, Consequence: ReadOnly, Cancellable: true, Policy: "manager.scan", Audit: "manager.scan", ResultSchema: "scan.v1", Examples: []string{"/scan", "/scan status scan-id"}, Help: "Bounded read-only Aegis-native core scan. Host modules require real adapters.", Base: true},
		{Name: "watch", Usage: "/watch [start|list|status|events|stop]", Grammar: "closed watch form", Class: LeasedSubscription, States: active, Prerequisites: []string{"Aegis-owned event source manager"}, Scopes: []string{"manager-session"}, Consequence: ReadOnly, Cancellable: true, Policy: "manager.watch", Audit: "manager.watch", ResultSchema: "watch.v1", Help: "Leased observation source; unavailable until a production source manager is configured.", Base: true},
		{Name: "findings", Usage: "/findings [finding-id]", Grammar: "optional opaque finding ID", Class: ImmediateRead, States: active, Prerequisites: []string{"finding store"}, Consequence: ReadOnly, Policy: "manager.findings", Audit: "manager.findings", ResultSchema: "findings.v1", Help: "Bounded disclosure-filtered open finding list or typed detail.", Base: true},
		{Name: "investigate", Usage: "/investigate [finding-id]", Grammar: "optional exact finding ID", Class: WorkflowMutation, States: active, Prerequisites: []string{"finding store"}, Consequence: Mutation, Cancellable: true, Policy: "manager.investigate", Audit: "manager.investigate", ResultSchema: "investigation.v1", Help: "List active investigations or create/attach after exact visible finding resolution.", Base: true},
		{Name: "timeline", Usage: "/timeline", Grammar: "no arguments", Class: ImmediateRead, States: active, Prerequisites: []string{"authoritative audit/event records"}, Consequence: ReadOnly, Policy: "manager.timeline", Audit: "manager.timeline", ResultSchema: "timeline.v1", Help: "Bounded authoritative event query with anchor, clock, order, and gap caveats.", Base: true},
		{Name: "report", Usage: "/report [investigation-id]", Grammar: "optional exact investigation ID", Class: Export, States: active, Consequence: Mutation, Policy: "manager.report", Audit: "manager.report", ResultSchema: "report.v1", Help: "Generate a frozen local report revision; never exports or publishes.", Base: true},
		{Name: "audit", Usage: "/audit [verify]", Grammar: "optional exact verify subcommand", Class: ImmediateRead, States: active, Prerequisites: []string{"audit authority"}, Consequence: ReadOnly, Policy: "manager.audit", Audit: "manager.audit", ResultSchema: "audit.v1", Help: "Bounded metadata-only audit listing or authoritative chain verification.", Base: true},
		{Name: "cancel", Usage: "/cancel [operation-id]", Grammar: "optional exact operation ID", Class: Lifecycle, States: active, Consequence: ReadOnly, Policy: "manager.cancel", Audit: "manager.cancel", ResultSchema: "cancel.v1", Help: "Request cancellation without guessing or claiming rollback.", Base: true},
		{Name: "clear", Usage: "/clear", Grammar: "no arguments", Class: Lifecycle, States: all, Consequence: Presentation, Policy: "manager.clear", Audit: "manager.clear", ResultSchema: "clear.v1", Help: "Clear/redraw local presentation only; authority and durable state remain unchanged.", Base: true},
		{Name: "exit", Aliases: []string{"quit"}, Usage: "/exit", Grammar: "no arguments", Class: Lifecycle, States: []LifecycleState{Startup, Degraded, Active, Expiring, Revoked, Cleanup}, Consequence: SessionClose, Policy: "manager.exit", Audit: "manager.exit", ResultSchema: "exit.v1", Help: "Enter the one bounded manager cleanup and terminal-restoration path.", Base: true},
		{Name: "secret", Usage: "/secret list [query] | /secret show <record-id>", Grammar: "manager credential metadata compatibility forms", Class: ImmediateRead, States: active, Prerequisites: []string{"credential authority"}, Consequence: ReadOnly, Policy: "manager.secret.metadata", Audit: "manager.secret.metadata", ResultSchema: "secret.metadata.v1", Help: "Compatibility extension for credential metadata only.", Base: false},
		{Name: "complete", Usage: "/complete <prefix>", Grammar: "one command prefix", Class: ImmediateRead, States: all, Consequence: ReadOnly, Policy: "manager.complete", Audit: "manager.complete", ResultSchema: "completion.v1", Help: "Compatibility/testing hook delegating to registry completion.", Base: false},
	}
	r := &Registry{definitions: definitions, byName: make(map[string]int), aliases: make(map[string]string)}
	for i, definition := range definitions {
		if definition.Name == "" || definition.Name != strings.ToLower(definition.Name) || strings.ContainsAny(definition.Name, " \t/\n") {
			return nil, fmt.Errorf("invalid command name %q", definition.Name)
		}
		if _, exists := r.byName[definition.Name]; exists {
			return nil, fmt.Errorf("duplicate command %q", definition.Name)
		}
		r.byName[definition.Name] = i
	}
	for _, definition := range definitions {
		for _, alias := range definition.Aliases {
			if _, exists := r.byName[alias]; exists || r.aliases[alias] != "" {
				return nil, fmt.Errorf("duplicate alias %q", alias)
			}
			r.aliases[alias] = definition.Name
		}
	}
	if len(r.BaseNames()) != 15 {
		return nil, fmt.Errorf("base registry must contain exactly 15 commands")
	}
	return r, nil
}

func (r *Registry) Definitions() []Definition {
	return append([]Definition(nil), r.definitions...)
}

func (r *Registry) BaseNames() []string {
	var names []string
	for _, definition := range r.definitions {
		if definition.Base {
			names = append(names, definition.Name)
		}
	}
	return names
}

func (r *Registry) Lookup(name string) (Definition, bool) {
	canonical := name
	if alias := r.aliases[name]; alias != "" {
		canonical = alias
	}
	index, ok := r.byName[canonical]
	if !ok {
		return Definition{}, false
	}
	return r.definitions[index], true
}

func (r *Registry) Available(definition Definition, state LifecycleState, prerequisites map[string]bool) (bool, string) {
	found := false
	for _, allowed := range definition.States {
		if allowed == state {
			found = true
			break
		}
	}
	if !found {
		return false, "lifecycle_state_unavailable"
	}
	for _, prerequisite := range definition.Prerequisites {
		if !prerequisites[prerequisite] {
			return false, "missing_" + strings.ReplaceAll(prerequisite, " ", "_")
		}
	}
	return true, "available"
}

func (r *Registry) Complete(prefix string, state LifecycleState, prerequisites map[string]bool) []string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "/"
	}
	var result []string
	for _, definition := range r.definitions {
		available, _ := r.Available(definition, state, prerequisites)
		if !available && definition.Name != "help" && definition.Name != "limits" && definition.Name != "exit" {
			continue
		}
		candidate := "/" + definition.Name
		if strings.HasPrefix(candidate, prefix) {
			result = append(result, candidate)
		}
		for _, alias := range definition.Aliases {
			candidate = "/" + alias
			if strings.HasPrefix(candidate, prefix) {
				result = append(result, candidate)
			}
		}
	}
	sort.Strings(result)
	return result
}
