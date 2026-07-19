package slash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/store"
)

type Scope struct {
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
}

type Result struct {
	Schema         string         `json:"schema"`
	ResultSchema   string         `json:"result_schema"`
	Operation      string         `json:"operation"`
	OperationID    string         `json:"operation_id,omitempty"`
	State          string         `json:"state"`
	Reason         string         `json:"reason"`
	ActorID        string         `json:"actor_id,omitempty"`
	ContextID      string         `json:"context_id,omitempty"`
	StanzaID       string         `json:"stanza_id,omitempty"`
	RequestedScope *Scope         `json:"requested_scope,omitempty"`
	EffectiveScope *Scope         `json:"effective_scope,omitempty"`
	StartedAt      time.Time      `json:"started_at"`
	FinishedAt     time.Time      `json:"finished_at,omitempty"`
	ObservedAt     time.Time      `json:"observed_at,omitempty"`
	Health         string         `json:"health,omitempty"`
	Coverage       string         `json:"coverage,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
	RelatedIDs     []string       `json:"related_ids,omitempty"`
	AuditReference string         `json:"audit_reference,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
}

type Operation struct {
	ID           string    `json:"id"`
	Canonical    string    `json:"canonical"`
	OwnerID      string    `json:"owner_id"`
	StanzaID     string    `json:"stanza_id"`
	State        string    `json:"state"`
	Cancellable  bool      `json:"cancellable"`
	Cancellation string    `json:"cancellation,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
}

type Scan struct {
	Schema           string            `json:"schema"`
	ID               string            `json:"id"`
	OperationID      string            `json:"operation_id"`
	OwnerID          string            `json:"owner_id"`
	StanzaID         string            `json:"stanza_id"`
	Outcome          string            `json:"outcome"`
	Profile          string            `json:"profile"`
	ProfileRevision  string            `json:"profile_revision"`
	RuleRevision     string            `json:"rule_revision"`
	PolicyGeneration string            `json:"policy_generation"`
	SourceGeneration string            `json:"source_generation"`
	InputIdentity    string            `json:"input_identity"`
	EquivalenceKey   string            `json:"equivalence_key"`
	RequestedScope   Scope             `json:"requested_scope"`
	EffectiveScope   Scope             `json:"effective_scope"`
	IncludedModules  []string          `json:"included_modules"`
	OmittedModules   []string          `json:"omitted_modules"`
	SourceHealth     map[string]string `json:"source_health"`
	Observations     []Observation     `json:"observations"`
	FindingIDs       []string          `json:"finding_ids"`
	Gaps             []string          `json:"gaps"`
	StartedAt        time.Time         `json:"started_at"`
	FinishedAt       time.Time         `json:"finished_at"`
}

type Observation struct {
	Module string `json:"module"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type FindingHistory struct {
	State  string    `json:"state"`
	At     time.Time `json:"at"`
	Actor  string    `json:"actor"`
	Reason string    `json:"reason"`
}

type Finding struct {
	Schema        string           `json:"schema"`
	ID            string           `json:"id"`
	OwnerID       string           `json:"owner_id"`
	StanzaID      string           `json:"stanza_id"`
	RuleID        string           `json:"rule_id"`
	RuleVersion   string           `json:"rule_version"`
	Severity      string           `json:"severity"`
	Confidence    string           `json:"confidence"`
	State         string           `json:"state"`
	History       []FindingHistory `json:"history"`
	Target        string           `json:"target"`
	Source        string           `json:"source"`
	Scope         Scope            `json:"scope"`
	Health        string           `json:"health"`
	Coverage      string           `json:"coverage"`
	EvidenceRefs  []string         `json:"evidence_refs"`
	RelatedIDs    []string         `json:"related_ids"`
	FirstObserved time.Time        `json:"first_observed"`
	LastObserved  time.Time        `json:"last_observed"`
}

type Investigation struct {
	Schema       string    `json:"schema"`
	ID           string    `json:"id"`
	OwnerID      string    `json:"owner_id"`
	StanzaID     string    `json:"stanza_id"`
	FindingIDs   []string  `json:"finding_ids"`
	State        string    `json:"state"`
	Scope        Scope     `json:"scope"`
	EvidenceRefs []string  `json:"evidence_refs"`
	Notes        []string  `json:"notes"`
	Hypotheses   []string  `json:"hypotheses"`
	Provenance   string    `json:"provenance"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Report struct {
	Schema             string    `json:"schema"`
	ID                 string    `json:"id"`
	Revision           uint64    `json:"revision"`
	OwnerID            string    `json:"owner_id"`
	StanzaID           string    `json:"stanza_id"`
	InvestigationID    string    `json:"investigation_id"`
	InputDigest        string    `json:"input_digest"`
	Scope              Scope     `json:"scope"`
	Coverage           string    `json:"coverage"`
	FindingIDs         []string  `json:"finding_ids"`
	EvidenceAppendix   []string  `json:"evidence_appendix"`
	TimelineReferences []string  `json:"timeline_references"`
	AuditReferences    []string  `json:"audit_references"`
	Unresolved         []string  `json:"unresolved_questions"`
	Provenance         string    `json:"provenance"`
	CreatedAt          time.Time `json:"created_at"`
	Digest             string    `json:"digest"`
}

type Context struct {
	Subject        core.Subject
	StanzaID       string
	MandateID      string
	MandateIssued  time.Time
	MandateExpiry  time.Time
	MandateState   string
	Lifecycle      LifecycleState
	RuntimeState   string
	Route          string
	PolicyVersion  string
	PolicyDigest   string
	Readiness      map[string]string
	Conversational bool
}

type Service struct {
	app      *app.Service
	registry *Registry
	now      func() time.Time
	mu       sync.Mutex
	active   map[string]context.CancelFunc
	attached map[string]string
}

func NewService(application *app.Service, registry *Registry) *Service {
	return &Service{app: application, registry: registry, now: func() time.Time { return time.Now().UTC() }, active: make(map[string]context.CancelFunc), attached: make(map[string]string)}
}

func (s *Service) Execute(ctx context.Context, manager Context, request Request) (Result, error) {
	now := s.now()
	result := Result{Schema: SchemaVersion, ResultSchema: request.Definition.ResultSchema, Operation: request.Definition.Policy, State: "denied", Reason: "authority_denied", ActorID: manager.Subject.ID, ContextID: manager.MandateID, StanzaID: manager.StanzaID, StartedAt: now, ObservedAt: now}
	if manager.Subject.ID == "" || manager.Subject.PrincipalID == "" || manager.Subject.PrincipalID != s.app.Config.Principal.ID {
		return result, app.ErrDenied
	}
	if manager.StanzaID != managerdomain.SecurityContext || manager.MandateID == "" {
		result.Reason = "exactly_one_stanza_required"
		return result, app.ErrDenied
	}
	if manager.MandateState == "revoked" || manager.Lifecycle == Revoked {
		result.Reason = "mandate_revoked"
		return result, app.ErrDenied
	}
	if !now.Before(manager.MandateExpiry) || !now.Before(manager.Subject.ExpiresAt) {
		result.Reason = "mandate_expired"
		return result, app.ErrExpired
	}
	prerequisites := prerequisites(manager)
	available, reason := s.registry.Available(request.Definition, manager.Lifecycle, prerequisites)
	if !available && request.Canonical != "watch" && request.Canonical != "scan" {
		result.State, result.Reason, result.FinishedAt = "unavailable", reason, s.now()
		if auditErr := s.app.AuditManagerCommand(ctx, manager.Subject, request.Definition.Audit, result.State, result.Reason, "", ""); auditErr != nil {
			return result, auditErr
		}
		return result, nil
	}
	var err error
	switch request.Canonical {
	case "help":
		result = s.help(result, manager, request)
	case "status":
		result = s.status(result, manager)
	case "context":
		result = s.contextResult(result, manager)
	case "authority":
		result = s.authority(result, manager, request)
	case "limits":
		result = s.limits(result, manager)
	case "scan":
		result, err = s.scan(ctx, result, manager, request)
	case "watch":
		result.State, result.Reason, result.Health = "unavailable", "watch_source_manager_unavailable", "unknown"
		result.Warnings = []string{"No host or Aegis event-source watch manager is configured; no watch was started and no events are claimed."}
	case "findings":
		result, err = s.findings(result, manager, request)
	case "investigate":
		result, err = s.investigate(result, manager, request)
	case "timeline":
		result, err = s.timeline(result, manager)
	case "report":
		result, err = s.report(result, manager, request)
	case "audit":
		result, err = s.audit(ctx, result, manager, request)
	case "cancel":
		result, err = s.cancel(result, manager, request)
	case "clear":
		result.State, result.Reason = "completed", "presentation_only"
		result.Data = map[string]any{"unchanged": []string{"session authority", "Hermes/model context", "operations", "watches", "findings", "investigations", "reports", "audit"}, "terminal_scrollback_erased": false}
	case "exit":
		result.State, result.Reason = "accepted", "bounded_cleanup_requested"
	default:
		return result, errors.New("extension execution is owned by the manager compatibility adapter")
	}
	result.FinishedAt = s.now()
	scopeDigest := ""
	if result.EffectiveScope != nil {
		scopeDigest = result.EffectiveScope.Digest
	}
	if auditErr := s.app.AuditManagerCommand(ctx, manager.Subject, request.Definition.Audit, result.State, result.Reason, result.OperationID, scopeDigest); auditErr != nil {
		return result, auditErr
	}
	return result, err
}

func prerequisites(manager Context) map[string]bool {
	return map[string]bool{
		"authenticated manager context":     manager.Subject.PrincipalID != "" && manager.StanzaID == managerdomain.SecurityContext,
		"Aegis-owned event source manager":  false,
		"finding store":                     true,
		"attached investigation":            false,
		"authoritative audit/event records": true,
		"audit authority":                   true,
		"credential authority":              manager.Readiness["authority"] == "ready",
	}
}

func completed(result Result, reason string, data map[string]any) Result {
	result.State, result.Reason, result.Data = "completed", reason, data
	return result
}

func (s *Service) help(result Result, manager Context, request Request) Result {
	state := prerequisites(manager)
	if len(request.Arguments) == 1 {
		topic := strings.TrimPrefix(request.Arguments[0], "/")
		if definition, ok := s.registry.Lookup(topic); ok {
			available, reason := s.registry.Available(definition, manager.Lifecycle, state)
			return completed(result, "registry_help", map[string]any{"name": "/" + definition.Name, "aliases": definition.Aliases, "usage": definition.Usage, "grammar": definition.Grammar, "class": definition.Class, "consequence": definition.Consequence, "cancellable": definition.Cancellable, "policy_operation": definition.Policy, "audit_operation": definition.Audit, "result_schema": definition.ResultSchema, "examples": definition.Examples, "help": definition.Help, "available": available, "availability_reason": reason})
		}
		switch topic {
		case "syntax":
			return completed(result, "registry_help", map[string]any{"syntax": "/<command> [<subcommand>] [typed literal arguments]", "quoting": "single and double quoted literals; quote/backslash escapes only inside quotes", "denied": []string{"shell expansion", "environment expansion", "pipes", "redirects", "chaining", "background execution", "! shell mode"}})
		case "keyboard":
			return completed(result, "registry_help", map[string]any{"keyboard": []string{"Enter submits", "Ctrl+J inserts newline", "? on empty input opens help", "Ctrl+L redraws", "Ctrl-C interrupts or exits according to lifecycle", "Ctrl-D on empty input exits"}})
		case "aliases":
			aliases := map[string]string{}
			for _, definition := range s.registry.Definitions() {
				for _, alias := range definition.Aliases {
					aliases["/"+alias] = "/" + definition.Name
				}
			}
			return completed(result, "registry_help", map[string]any{"aliases": aliases})
		case "states":
			return completed(result, "registry_help", map[string]any{"states": []LifecycleState{Startup, Degraded, Active, Expiring, Revoked, Cleanup}})
		}
		result.State, result.Reason = "failed", "unknown_help_topic"
		return result
	}
	var commands []map[string]any
	for _, definition := range s.registry.Definitions() {
		if !definition.Base {
			continue
		}
		available, reason := s.registry.Available(definition, manager.Lifecycle, state)
		commands = append(commands, map[string]any{"name": "/" + definition.Name, "usage": definition.Usage, "class": definition.Class, "consequence": definition.Consequence, "available": available, "reason": reason})
	}
	return completed(result, "registry_help", map[string]any{"commands": commands, "literal_slash": "// at command position becomes one leading slash and still passes the ordinary ingress guard"})
}

func (s *Service) status(result Result, manager Context) Result {
	readiness := map[string]string{}
	for key, value := range manager.Readiness {
		readiness[key] = value
	}
	return completed(result, "authoritative_status", map[string]any{"freshness": "observed_now", "lifecycle": manager.Lifecycle, "identity": "authenticated", "stanza": manager.StanzaID, "mandate": manager.MandateState, "runtime": manager.RuntimeState, "route": manager.Route, "conversational": manager.Conversational, "readiness": readiness, "foreground_operations": s.operationsFor(manager, false), "watch_state": "unavailable", "finding_count": "not_disclosed_in_status", "unknown_is_not_zero": true})
}

func (s *Service) contextResult(result Result, manager Context) Result {
	return completed(result, "authoritative_context", map[string]any{"subject_id": manager.Subject.ID, "principal_id": manager.Subject.PrincipalID, "authentication_issuer": manager.Subject.Issuer, "authentication_method": manager.Subject.Method, "authenticated_at": manager.Subject.AuthenticatedAt, "logical_agent": managerdomain.LogicalAgentID, "stanza": manager.StanzaID, "policy_revision": manager.PolicyVersion, "policy_digest": manager.PolicyDigest, "mandate_id": manager.MandateID, "mandate_issued_at": manager.MandateIssued, "mandate_expires_at": manager.MandateExpiry, "mandate_state": manager.MandateState, "runtime": "Hermes Agent", "runtime_state": manager.RuntimeState, "default_scope": "manager-session", "isolation_limits": []string{"Hermes home/process state isolation is not a host sandbox", "unmediated host filesystem and network routes are not confined"}})
}

func (s *Service) authority(result Result, manager Context, request Request) Result {
	var decisions []map[string]any
	definitions := s.registry.Definitions()
	if len(request.Arguments) == 1 {
		definition, ok := s.registry.Lookup(strings.TrimPrefix(request.Arguments[0], "/"))
		if !ok {
			result.State, result.Reason = "failed", "unknown_operation"
			return result
		}
		definitions = []Definition{definition}
	}
	for _, definition := range definitions {
		if !definition.Base {
			continue
		}
		available, reason := s.registry.Available(definition, manager.Lifecycle, prerequisites(manager))
		decisions = append(decisions, map[string]any{"operation": definition.Policy, "available": available, "reason": reason, "scope": definition.Scopes, "consequence": definition.Consequence, "approval": "not_granted_by_slash_command", "delegation": "disabled", "expires_at": manager.MandateExpiry, "policy_revision": manager.PolicyVersion})
	}
	return completed(result, "authority_explained", map[string]any{"decisions": decisions, "grants_changed": false, "stanza_switched": false, "authority_unioned": false})
}

func (s *Service) limits(result Result, manager Context) Result {
	return completed(result, "limits_reported", map[string]any{"missing_sources": []string{"host process sensor", "host network sensor", "filesystem sensor", "persistence sensor", "dependency scanner", "leased event-source manager"}, "scan_bounds": map[string]any{"scope": "manager-session", "profile": "core", "host_expansion": false}, "watch": "unavailable", "retention": "bounded Aegis state store; no external evidence sink", "delegation": "disabled", "report_export": "disabled; local frozen artifacts only", "runtime_isolation": "disposable Hermes state is not a host sandbox", "unmediated_routes": "host filesystem/network/process routes outside an Aegis gateway may remain available", "uncertainty": "absence of observations outside covered Aegis-native checks is unknown, not zero"})
}

func (s *Service) scan(ctx context.Context, result Result, manager Context, request Request) (Result, error) {
	if len(request.Arguments) > 0 && request.Arguments[0] == "list" {
		scans, err := s.listScans(manager)
		if err != nil {
			return result, err
		}
		return completed(result, "scan_list", map[string]any{"scans": scans}), nil
	}
	if len(request.Arguments) > 0 && request.Arguments[0] == "status" {
		scan, err := s.getVisibleScan(manager, request.Arguments[1])
		if err != nil {
			result.State, result.Reason = "failed", "scan_not_found"
			return result, nil
		}
		return completed(result, "scan_status", map[string]any{"scan": scan}), nil
	}
	profile := "core"
	if len(request.Arguments) == 1 {
		profile = request.Arguments[0]
	}
	if profile != "core" {
		result.State, result.Reason, result.Health = "unavailable", "scan_adapter_unavailable", "unknown"
		result.Warnings = []string{"The requested host module has no production adapter; no host scan was run."}
		return result, nil
	}
	scope := Scope{Kind: "manager-session", Digest: core.Digest(map[string]string{"owner": manager.Subject.ID, "stanza": manager.StanzaID, "mandate": manager.MandateID, "policy": manager.PolicyDigest})}
	inputIdentity := core.Digest(map[string]any{"runtime": manager.RuntimeState, "readiness": manager.Readiness, "route": manager.Route})
	equivalence := core.Digest(map[string]string{"owner": manager.Subject.ID, "scope": scope.Digest, "profile": "core-v1", "rules": "manager-core-rules-v1", "policy": manager.PolicyDigest, "source": "aegis-local-v1", "input": inputIdentity})
	if existing, ok := s.equivalentScan(manager, equivalence); ok {
		result.OperationID, result.State, result.Reason = existing.OperationID, "completed", "equivalent_scan_attached"
		result.RequestedScope, result.EffectiveScope = &scope, &scope
		result.Data = map[string]any{"scan": existing}
		return result, nil
	}
	op := Operation{ID: store.ID("operation"), Canonical: "manager.scan", OwnerID: manager.Subject.ID, StanzaID: manager.StanzaID, State: "running", Cancellable: true, StartedAt: s.now()}
	s.saveOperation(op)
	scan := Scan{Schema: "aegis.scan.v1", ID: store.ID("scan"), OperationID: op.ID, OwnerID: manager.Subject.ID, StanzaID: manager.StanzaID, Profile: "core", ProfileRevision: "core-v1", RuleRevision: "manager-core-rules-v1", PolicyGeneration: manager.PolicyDigest, SourceGeneration: "aegis-local-v1", InputIdentity: inputIdentity, EquivalenceKey: equivalence, RequestedScope: scope, EffectiveScope: scope, IncludedModules: []string{"identity-authority", "runtime-configuration", "tool-credential-memory-broker-scope", "route-policy", "control-readiness", "audit-chain"}, OmittedModules: []string{"host-processes", "host-network", "host-files", "host-persistence", "dependencies", "endpoint-sensors"}, SourceHealth: map[string]string{}, StartedAt: s.now()}
	scan.Observations = append(scan.Observations,
		Observation{Module: "identity-authority", State: "observed", Detail: "authenticated principal is bound to exactly one built-in manager stanza and one unexpired authority context"},
		Observation{Module: "tool-credential-memory-broker-scope", State: "observed", Detail: "built-in manager uses no_mcp; ambient plugins, skills, MCP, profile, and arbitrary runtime tools are not granted"},
		Observation{Module: "route-policy", State: "observed", Detail: "configured manager route is local-only with cloud fallback and model switching disabled"},
	)
	scan.SourceHealth["identity-authority"] = "healthy"
	scan.SourceHealth["configuration"] = "healthy"
	runtimeDescriptor, runtimeErr := s.app.Runtime(ctx)
	if runtimeErr != nil {
		scan.SourceHealth["hermes-runtime"] = "unavailable"
		scan.Gaps = append(scan.Gaps, "Hermes executable/version could not be authoritatively discovered in this scan")
	} else {
		scan.SourceHealth["hermes-runtime"] = "healthy"
		scan.Observations = append(scan.Observations, Observation{Module: "runtime-configuration", State: "observed", Detail: fmt.Sprintf("Hermes Agent %s adapter %s was discovered", runtimeDescriptor.Version, runtimeDescriptor.AdapterVersion)})
	}
	if err := s.app.VerifyAuditAs(manager.Subject); err != nil {
		scan.SourceHealth["audit-chain"] = "degraded"
		scan.Gaps = append(scan.Gaps, "authoritative audit-chain verification failed or was unavailable")
		now := s.now()
		finding := Finding{Schema: "aegis.finding.v1", ID: store.ID("finding"), OwnerID: manager.Subject.ID, StanzaID: manager.StanzaID, RuleID: "aegis.audit.chain-verification", RuleVersion: "v1", Severity: "high", Confidence: "high", State: "open", Target: "Aegis audit chain", Source: "Aegis audit verification service", Scope: scope, Health: "degraded", Coverage: "configured local Aegis audit chain", EvidenceRefs: []string{"audit-chain-verification-failure"}, FirstObserved: now, LastObserved: now}
		finding.History = []FindingHistory{{State: "open", At: now, Actor: manager.Subject.ID, Reason: "detected by Aegis audit verification"}}
		if saveErr := s.app.Store.Save("manager-findings", finding.ID, finding); saveErr != nil {
			return result, saveErr
		}
		scan.FindingIDs = append(scan.FindingIDs, finding.ID)
	} else {
		scan.SourceHealth["audit-chain"] = "healthy"
		scan.Observations = append(scan.Observations, Observation{Module: "audit-chain", State: "observed", Detail: "authoritative audit verification completed successfully"})
	}
	for key, value := range manager.Readiness {
		scan.Observations = append(scan.Observations, Observation{Module: "control-readiness", State: "observed", Detail: key + "=" + value})
	}
	scan.Gaps = append(scan.Gaps, "No endpoint sensors were installed or queried; host process, network, file, persistence, and dependency state are outside covered scope")
	if ctx.Err() != nil {
		scan.Outcome = "cancelled"
	} else if len(scan.FindingIDs) != 0 || runtimeErr != nil || scan.SourceHealth["audit-chain"] != "healthy" {
		scan.Outcome = "degraded"
	} else {
		scan.Outcome = "completed_no_findings"
	}
	scan.FinishedAt = s.now()
	op.State, op.FinishedAt = scan.Outcome, scan.FinishedAt
	if err := s.app.Store.Save("manager-scans", scan.ID, scan); err != nil {
		return result, err
	}
	s.saveOperation(op)
	result.OperationID, result.State, result.Reason = op.ID, scan.Outcome, scan.Outcome
	result.RequestedScope, result.EffectiveScope = &scope, &scope
	result.Health = healthForScan(scan)
	result.Coverage = "Aegis-native manager core checks only"
	if len(scan.FindingIDs) == 0 {
		result.Warnings = []string{"No findings in covered scope does not mean safe, secure, clean, protected, or threat-free."}
	} else {
		result.Warnings = []string{"Findings are authoritative only for the named Aegis-native rule and covered scope; omitted host modules remain unknown."}
	}
	result.RelatedIDs = append([]string{scan.ID}, scan.FindingIDs...)
	result.Data = map[string]any{"scan": scan, "summary": scanSummary(scan)}
	return result, nil
}

func healthForScan(scan Scan) string {
	if scan.Outcome == "degraded" || scan.Outcome == "partial" {
		return "degraded"
	}
	if scan.Outcome == "failed" {
		return "failed"
	}
	return "healthy"
}
func scanSummary(scan Scan) string {
	if scan.Outcome == "completed_no_findings" {
		return "no findings in covered scope"
	}
	return scan.Outcome
}

func (s *Service) saveOperation(operation Operation) {
	_ = s.app.Store.Save("manager-operations", operation.ID, operation)
}
func (s *Service) operationsFor(manager Context, includeTerminal bool) []Operation {
	var operations []Operation
	_ = s.app.Store.List("manager-operations", func(raw json.RawMessage) error {
		var operation Operation
		if decodeRecord(raw, &operation) == nil && operation.OwnerID == manager.Subject.ID && operation.StanzaID == manager.StanzaID && (includeTerminal || operation.FinishedAt.IsZero()) {
			operations = append(operations, operation)
		}
		return nil
	})
	sort.Slice(operations, func(i, j int) bool { return operations[i].StartedAt.Before(operations[j].StartedAt) })
	return operations
}
func (s *Service) listScans(manager Context) ([]Scan, error) {
	var scans []Scan
	err := s.app.Store.List("manager-scans", func(raw json.RawMessage) error {
		var scan Scan
		if err := decodeRecord(raw, &scan); err != nil {
			return err
		}
		if scan.Schema != "aegis.scan.v1" || scan.ID == "" || scan.OperationID == "" {
			return errors.New("invalid durable scan schema")
		}
		if scan.OwnerID == manager.Subject.ID && scan.StanzaID == manager.StanzaID {
			scans = append(scans, scan)
		}
		return nil
	})
	sort.Slice(scans, func(i, j int) bool { return scans[i].StartedAt.After(scans[j].StartedAt) })
	return scans, err
}
func (s *Service) equivalentScan(manager Context, key string) (Scan, bool) {
	scans, _ := s.listScans(manager)
	for _, scan := range scans {
		if scan.EquivalenceKey == key {
			return scan, true
		}
	}
	return Scan{}, false
}
func (s *Service) getVisibleScan(manager Context, id string) (Scan, error) {
	var scan Scan
	if err := s.app.Store.Load("manager-scans", id, &scan); err != nil {
		return scan, err
	}
	if scan.Schema != "aegis.scan.v1" || scan.ID != id || scan.OwnerID != manager.Subject.ID || scan.StanzaID != manager.StanzaID {
		return Scan{}, app.ErrDenied
	}
	return scan, nil
}

func (s *Service) findings(result Result, manager Context, request Request) (Result, error) {
	findings, err := s.visibleFindings(manager)
	if err != nil {
		return result, err
	}
	if len(request.Arguments) == 0 {
		return completed(result, "visible_open_findings", map[string]any{"findings": findings, "count": len(findings), "sort": "severity_then_last_observed_desc", "limit": 50}), nil
	}
	for _, finding := range findings {
		if finding.ID == request.Arguments[0] {
			return completed(result, "finding_detail", map[string]any{"finding": finding}), nil
		}
	}
	result.State, result.Reason = "failed", "finding_not_found_or_not_visible"
	return result, nil
}
func (s *Service) visibleFindings(manager Context) ([]Finding, error) {
	var findings []Finding
	err := s.app.Store.List("manager-findings", func(raw json.RawMessage) error {
		var finding Finding
		if err := decodeRecord(raw, &finding); err != nil {
			return err
		}
		if finding.Schema != "aegis.finding.v1" || finding.ID == "" || finding.RuleID == "" || len(finding.History) == 0 {
			return errors.New("invalid durable finding schema")
		}
		if finding.OwnerID == manager.Subject.ID && finding.StanzaID == manager.StanzaID && finding.State == "open" {
			findings = append(findings, finding)
		}
		return nil
	})
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Severity == findings[j].Severity {
			return findings[i].LastObserved.After(findings[j].LastObserved)
		}
		return findings[i].Severity > findings[j].Severity
	})
	if len(findings) > 50 {
		findings = findings[:50]
	}
	return findings, err
}

func (s *Service) investigate(result Result, manager Context, request Request) (Result, error) {
	if len(request.Arguments) == 0 {
		investigations, err := s.visibleInvestigations(manager)
		if err != nil {
			return result, err
		}
		reason := "active_investigations"
		if len(investigations) == 0 {
			reason = "no_active_investigations_no_target_selected"
		}
		return completed(result, reason, map[string]any{"investigations": investigations, "usage": "/investigate <finding-id>", "target_selected": false}), nil
	}
	findings, err := s.visibleFindings(manager)
	if err != nil {
		return result, err
	}
	var target *Finding
	for i := range findings {
		if findings[i].ID == request.Arguments[0] {
			target = &findings[i]
			break
		}
	}
	if target == nil {
		result.State, result.Reason = "failed", "finding_not_found_or_not_visible"
		return result, nil
	}
	investigations, _ := s.visibleInvestigations(manager)
	for _, existing := range investigations {
		if len(existing.FindingIDs) == 1 && existing.FindingIDs[0] == target.ID {
			s.mu.Lock()
			s.attached[manager.Subject.ID] = existing.ID
			s.mu.Unlock()
			return completed(result, "investigation_attached", map[string]any{"investigation": existing}), nil
		}
	}
	now := s.now()
	investigation := Investigation{Schema: "aegis.investigation.v1", ID: store.ID("investigation"), OwnerID: manager.Subject.ID, StanzaID: manager.StanzaID, FindingIDs: []string{target.ID}, State: "active", Scope: target.Scope, EvidenceRefs: append([]string(nil), target.EvidenceRefs...), Provenance: "deterministic Aegis finding linkage; no model authority", CreatedAt: now, ExpiresAt: minTime(manager.MandateExpiry, now.Add(24*time.Hour))}
	if err = s.app.Store.Save("manager-investigations", investigation.ID, investigation); err != nil {
		return result, err
	}
	s.mu.Lock()
	s.attached[manager.Subject.ID] = investigation.ID
	s.mu.Unlock()
	return completed(result, "investigation_created", map[string]any{"investigation": investigation}), nil
}
func (s *Service) visibleInvestigations(manager Context) ([]Investigation, error) {
	var values []Investigation
	err := s.app.Store.List("manager-investigations", func(raw json.RawMessage) error {
		var value Investigation
		if err := decodeRecord(raw, &value); err != nil {
			return err
		}
		if value.Schema != "aegis.investigation.v1" || value.ID == "" || len(value.FindingIDs) == 0 {
			return errors.New("invalid durable investigation schema")
		}
		if value.OwnerID == manager.Subject.ID && value.StanzaID == manager.StanzaID && value.State == "active" {
			values = append(values, value)
		}
		return nil
	})
	sort.Slice(values, func(i, j int) bool { return values[i].CreatedAt.After(values[j].CreatedAt) })
	return values, err
}

func (s *Service) timeline(result Result, manager Context) (Result, error) {
	events, err := s.app.AuditEventsAs(manager.Subject)
	if err != nil {
		return result, err
	}
	if len(events) > 50 {
		events = events[len(events)-50:]
	}
	entries := make([]map[string]any, 0, len(events))
	for _, event := range events {
		entries = append(entries, map[string]any{"event_id": event.ID, "event_time": event.OccurredAt, "source": "aegis-authoritative-audit", "type": event.Type, "outcome": event.Outcome, "reason": event.Reason, "related_ids": []string{event.SessionID, event.MandateID, event.ApprovalID, event.ProvisioningID}})
	}
	return completed(result, "authoritative_recent_timeline", map[string]any{"anchor": "current manager authority session", "range": "bounded most recent 50 visible authoritative events", "entries": entries, "ordering": "audit append order", "clock_caveat": "event time uses the local Aegis authority clock", "gaps": "external and host event sources are absent; audit verification does not prove independent retention", "disclosure": "metadata-only visible principal events"}), nil
}

func (s *Service) report(result Result, manager Context, request Request) (Result, error) {
	id := ""
	if len(request.Arguments) == 1 {
		id = request.Arguments[0]
	} else {
		s.mu.Lock()
		id = s.attached[manager.Subject.ID]
		s.mu.Unlock()
	}
	if id == "" {
		investigations, err := s.visibleInvestigations(manager)
		if err != nil {
			return result, err
		}
		result.State, result.Reason = "unavailable", "explicit_investigation_required"
		result.Data = map[string]any{"eligible_investigations": investigations, "exported": false}
		return result, nil
	}
	var investigation Investigation
	if err := s.app.Store.Load("manager-investigations", id, &investigation); err != nil || investigation.Schema != "aegis.investigation.v1" || investigation.ID != id || investigation.OwnerID != manager.Subject.ID || investigation.StanzaID != manager.StanzaID {
		result.State, result.Reason = "failed", "investigation_not_found_or_not_visible"
		return result, nil
	}
	now := s.now()
	report := Report{Schema: "aegis.report.v1", ID: store.ID("report"), Revision: s.nextReportRevision(investigation.ID), OwnerID: manager.Subject.ID, StanzaID: manager.StanzaID, InvestigationID: investigation.ID, InputDigest: core.Digest(investigation), Scope: investigation.Scope, Coverage: "linked Aegis-native evidence references only", FindingIDs: append([]string(nil), investigation.FindingIDs...), EvidenceAppendix: append([]string(nil), investigation.EvidenceRefs...), Unresolved: []string{"Host process, network, file, persistence, and dependency evidence is unavailable"}, Provenance: "deterministic local Aegis generation; no model or subagent narrative", CreatedAt: now}
	events, _ := s.app.AuditEventsAs(manager.Subject)
	for _, event := range events {
		if event.ID != "" {
			report.AuditReferences = append(report.AuditReferences, event.ID)
		}
	}
	if len(report.AuditReferences) > 20 {
		report.AuditReferences = report.AuditReferences[len(report.AuditReferences)-20:]
	}
	report.Digest = core.Digest(report)
	if err := s.app.Store.Save("manager-reports", report.ID, report); err != nil {
		return result, err
	}
	return completed(result, "local_frozen_report_created", map[string]any{"report": report, "exported": false, "published": false}), nil
}
func (s *Service) nextReportRevision(investigation string) uint64 {
	revision := uint64(1)
	_ = s.app.Store.List("manager-reports", func(raw json.RawMessage) error {
		var report Report
		if decodeRecord(raw, &report) == nil && report.Schema == "aegis.report.v1" && report.ID != "" && report.InvestigationID == investigation && report.Revision >= revision {
			revision = report.Revision + 1
		}
		return nil
	})
	return revision
}

func (s *Service) audit(ctx context.Context, result Result, manager Context, request Request) (Result, error) {
	if len(request.Arguments) == 1 {
		if err := s.app.VerifyAuditAs(manager.Subject); err != nil {
			result.State, result.Reason, result.Health = "failed", "audit_verification_failed", "failed"
			return result, nil
		}
		result.State, result.Reason, result.Health = "completed", "audit_chain_valid", "healthy"
		return result, nil
	}
	events, err := s.app.AuditEventsAs(manager.Subject)
	if err != nil {
		return result, err
	}
	if len(events) > 50 {
		events = events[len(events)-50:]
	}
	safe := make([]core.AuditEvent, 0, len(events))
	for _, event := range events {
		event.Metadata = filterAuditMetadata(event.Metadata)
		safe = append(safe, event)
	}
	return completed(result, "metadata_only_audit", map[string]any{"events": safe, "limit": 50, "verification": "not_run; use /audit verify"}), nil
}
func filterAuditMetadata(metadata map[string]string) map[string]string {
	allowed := map[string]bool{"action": true, "cleanup": true, "deployment_id": true, "destination": true, "mode": true, "model": true, "operation": true, "operation_id": true, "record_id": true, "request_id": true, "route_digest": true, "runtime": true, "scope_digest": true, "session_id": true}
	out := map[string]string{}
	for key, value := range metadata {
		if allowed[key] {
			out[key] = value
		}
	}
	return out
}

func (s *Service) cancel(result Result, manager Context, request Request) (Result, error) {
	operations := s.operationsFor(manager, false)
	if len(request.Arguments) == 0 {
		if len(operations) == 0 {
			return completed(result, "no_foreground_operation", map[string]any{"candidates": []Operation{}, "rollback_claimed": false}), nil
		}
		if len(operations) > 1 {
			result.State, result.Reason = "unavailable", "ambiguous_operation"
			result.Data = map[string]any{"candidates": operations, "rollback_claimed": false}
			return result, nil
		}
		request.Arguments = []string{operations[0].ID}
	}
	id := request.Arguments[0]
	for _, operation := range operations {
		if operation.ID == id {
			if !operation.Cancellable {
				result.State, result.Reason = "unavailable", "operation_not_cancellable"
				return result, nil
			}
			s.mu.Lock()
			cancel := s.active[id]
			s.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			operation.Cancellation = "requested"
			s.saveOperation(operation)
			result.State, result.Reason = "cancel_requested", "cancellation_requested"
			result.OperationID = id
			result.Data = map[string]any{"rollback_claimed": false, "history_deleted": false}
			return result, nil
		}
	}
	result.State, result.Reason = "failed", "operation_unknown_or_terminal"
	return result, nil
}

func decodeRecord(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing manager record JSON")
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
