package command

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/tui"
	"github.com/spf13/cobra"
)

type managerOperations struct {
	service   *app.Service
	subject   core.Subject
	authority *credentials.Authority
}

func (m managerOperations) Status(context.Context) (map[string]any, error) {
	return map[string]any{"manager": managerdomain.LogicalAgentID, "context": managerdomain.SecurityContext, "route": "local-only"}, nil
}
func (m managerOperations) List(ctx context.Context, q string, n int) ([]credentials.SecretRecord, error) {
	return m.authority.List(ctx, q, n)
}
func (m managerOperations) Counts(ctx context.Context) (credentials.SecretCounts, error) {
	return m.authority.Counts(ctx)
}
func (m managerOperations) ReadValue(ctx context.Context, reference string, consume func(credentials.SecretRecord, []byte) error) error {
	return m.authority.ReadValue(ctx, reference, func(record credentials.SecretRecord, value []byte) error {
		if err := m.service.AuditCredentialOperation(ctx, m.subject, "credential_value_read", "ok", "authenticated_manager_read_value", record.ID); err != nil {
			return err
		}
		return consume(record, value)
	})
}
func (m managerOperations) Metadata(ctx context.Context, id string) (credentials.SecretRecord, error) {
	return m.authority.Metadata(ctx, id)
}
func (m managerOperations) History(ctx context.Context, id string, limit int) ([]credentials.SecretVersionMetadata, error) {
	return m.authority.History(ctx, id, limit)
}
func (m managerOperations) Create(ctx context.Context, a managerdomain.CreateArguments, value []byte) (credentials.SecretRecord, error) {
	record, err := m.authority.Create(ctx, a.Reference, a.Kind, m.subject.PrincipalID, value)
	if err != nil {
		return record, err
	}
	if err = m.service.AuditCredentialOperation(ctx, m.subject, "credential_created", "ok", "manager_confirmed", record.ID); err != nil {
		return credentials.SecretRecord{}, err
	}
	return record, nil
}
func (m managerOperations) Rotate(ctx context.Context, a managerdomain.RotateArguments, value []byte) (credentials.SecretRecord, error) {
	record, err := m.authority.Rotate(ctx, a.RecordID, value)
	if err != nil {
		return record, err
	}
	if err = m.service.AuditCredentialOperation(ctx, m.subject, "credential_rotated", "ok", "manager_confirmed", record.ID); err != nil {
		return credentials.SecretRecord{}, err
	}
	return record, nil
}
func (m managerOperations) Revoke(ctx context.Context, a managerdomain.RevokeArguments) error {
	if err := m.authority.Revoke(ctx, a.RecordID, a.Version, a.Reason); err != nil {
		return err
	}
	return m.service.AuditCredentialOperation(ctx, m.subject, "credential_revoked", "ok", a.Reason, a.RecordID)
}
func (m managerOperations) Bind(ctx context.Context, a managerdomain.BindingArguments) error {
	binding := credentials.CredentialBinding{Key: credentials.CredentialBindingKey{AgentID: a.AgentID, StanzaID: a.StanzaID, DeploymentID: m.service.Config.Credentials.Authority.DeploymentID, Scope: a.Scope}, SecretRecord: a.RecordID, VersionPolicy: a.VersionPolicy, PinnedVersion: a.PinnedVersion, Mode: a.Mode, Destinations: a.Destinations, Enabled: true}
	if err := m.authority.Bind(ctx, binding); err != nil {
		return err
	}
	return m.service.AuditCredentialOperation(ctx, m.subject, "credential_bound", "ok", "manager_confirmed", a.RecordID)
}
func (m managerOperations) VerifyAudit(ctx context.Context) error { return m.service.VerifyAudit(ctx) }

type armedGateway struct {
	client    *managerdomain.GatewayClient
	budget    atomic.Int32
	sensitive *managerdomain.SensitiveTracker
}

func (g *armedGateway) Turn(ctx context.Context, session, text string, maximum int) ([]byte, error) {
	g.budget.Store(1)
	defer g.budget.Store(0)
	return g.client.Turn(ctx, session, text, maximum)
}
func (g *armedGateway) TurnStream(ctx context.Context, session, text string, maximum int, delta func([]byte) error) ([]byte, error) {
	g.budget.Store(1)
	defer g.budget.Store(0)
	return g.client.TurnStream(ctx, session, text, maximum, delta)
}
func (g *armedGateway) consume() bool                  { return g.budget.CompareAndSwap(1, 0) }
func (g *armedGateway) RegisterSensitive(value []byte) { g.sensitive.Add(value) }

type conversationalRuntime struct {
	session        *managerdomain.Session
	hermes         *managerdomain.HermesProcess
	proxy          *managerdomain.Proxy
	ollama         *managerdomain.OllamaClient
	managed        *managerdomain.ManagedOllama
	model          string
	authorityClose func() error
	active         atomic.Bool
	failures       chan error
	closeOnce      sync.Once
	closeErr       error
	testCleanup    []func(context.Context) error
	testFinalize   func(context.Context, string, string) error
	cleanupEvent   func(string, string)
	cleanupFailed  []string
}

func startConversationalManager(ctx context.Context, service *app.Service, subject core.Subject, guard *managerdomain.Guard, cmd *cobra.Command, input *terminalInput, presentation *tui.Controller, stage func(string)) (runtime *conversationalRuntime, err error) {
	if stage == nil {
		stage = func(string) {}
	}
	cfg := service.Config.Manager
	if !protectedIntakeCancellationSafe {
		return nil, errors.New(managerdomain.ReasonRuntimeUnsupported + ": cancellation-safe protected terminal intake is unavailable on this operating system")
	}
	if cfg.Inference.Model == "" {
		return nil, errors.New(managerdomain.ReasonModelAbsent)
	}
	authorityState := inspectManagerReadiness(service).authority
	if authorityState == "absent" {
		return nil, errors.New(managerdomain.ReasonAuthorityUnavailable)
	}
	if authorityState != "ready" {
		return nil, errors.New(managerdomain.ReasonAuthorityInvalid)
	}
	if cfg.Hermes.ContextLength < 65536 {
		return nil, errors.New(managerdomain.ReasonContextUnsupported)
	}
	endpoint := cfg.Inference.Endpoint
	runtime = &conversationalRuntime{model: cfg.Inference.Model, failures: make(chan error, 1)}
	if presentation != nil {
		runtime.cleanupEvent = func(stage, status string) {
			_ = presentation.Emit(tui.Event{Kind: tui.CleanupStage, Origin: tui.AegisAuthoritative, Stage: stage, Message: stage + ": " + status})
		}
	}
	fail := true
	defer func() {
		if fail && runtime != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), cfg.CleanupTimeout)
			defer cancel()
			if cleanupErr := runtime.Close(cleanup, managerdomain.EndStartupFailed); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("manager cleanup failed: %w", cleanupErr))
			}
		}
	}()
	stage("validating credential authority")
	authority, closeAuthority, err := openAuthorityForService(cmd, service)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonAuthorityInvalid, err)
	}
	runtime.authorityClose = closeAuthority
	stage("discovering Hermes Agent")
	descriptor, err := service.Hermes.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonRuntimeUnsupported, err)
	}
	if cfg.Inference.Mode == "managed" {
		stage("starting Aegis-managed Ollama")
		runtime.managed, err = managerdomain.StartManagedOllama(ctx, cfg.Inference.Executable, service.Config.StateDir, cfg.Inference.StartTimeout)
		if err != nil {
			return nil, err
		}
		endpoint = runtime.managed.Endpoint()
	}
	stage("verifying local Ollama route")
	runtime.ollama, err = managerdomain.NewOllamaClient(endpoint, cfg.Inference.RequestTimeout)
	if err != nil {
		return nil, err
	}
	ollamaVersion, err := runtime.ollama.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonOllamaUnavailable, err)
	}
	stage("verifying exact model artifact")
	if _, err = runtime.ollama.VerifyModel(ctx, cfg.Inference.Model, cfg.Inference.ModelDigest); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (!strings.Contains(err.Error(), managerdomain.ReasonModelAbsent) && !strings.Contains(err.Error(), managerdomain.ReasonDigestMismatch)) {
			return nil, fmt.Errorf("%s: %w", managerdomain.ReasonOllamaUnavailable, err)
		}
		return nil, err
	}
	stage("validating certification")
	certification, err := managerdomain.LoadCertification(cfg.Inference.Certification, cfg.Inference.Model, cfg.Inference.ModelDigest, descriptor.Version, ollamaVersion, cfg.Hermes.ContextLength)
	if err != nil {
		return nil, err
	}
	if err = runtime.ollama.Load(ctx, cfg.Inference.Model, cfg.Hermes.ContextLength, cfg.Inference.KeepAlive); err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonModelLoadFailed, err)
	}
	now := time.Now().UTC()
	route := managerdomain.RoutePlan{SchemaVersion: "aegis.manager.route.v1", ManagerID: managerdomain.LogicalAgentID, SecurityContext: managerdomain.SecurityContext, HermesPath: descriptor.Executable, HermesVersion: descriptor.Version, OllamaMode: cfg.Inference.Mode, OllamaEndpoint: endpoint, OllamaVersion: ollamaVersion, Model: certification.Identity(), ProxyIdentity: "ephemeral-session-capability", IssuedAt: now, ExpiresAt: subject.ExpiresAt}
	routeDigest, err := route.Digest()
	if err != nil {
		return nil, err
	}
	sensitive := &managerdomain.SensitiveTracker{}
	armed := &armedGateway{sensitive: sensitive}
	runtime.active.Store(true)
	stage("opening authenticated inference route")
	runtime.proxy, err = managerdomain.StartProxy(ctx, managerdomain.ProxyConfig{Target: endpoint, Model: cfg.Inference.Model, RouteDigest: routeDigest, MaximumRequestBytes: cfg.Inference.MaximumRequestBytes, MaximumResponseBytes: cfg.Inference.MaximumResponseBytes, Timeout: cfg.Inference.RequestTimeout, Guard: guard, SessionActive: runtime.active.Load, CapabilityExpires: subject.ExpiresAt, ConsumeCapability: armed.consume, RequireSystemInstruction: true, AllowPlaintextRequests: true, Sensitive: sensitive})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonRouteMismatch, err)
	}
	python := managerPython(descriptor.Installation, descriptor.Executable)
	if python == "" {
		return nil, errors.New(managerdomain.ReasonRuntimeUnsupported + ": Hermes gateway Python executable not found")
	}
	stage("starting disposable Hermes runtime")
	runtime.hermes, err = managerdomain.StartHermesProcess(ctx, managerdomain.HermesProcessConfig{Python: python, Installation: descriptor.Installation, StateRoot: service.Config.StateDir, ProxyEndpoint: runtime.proxy.Endpoint(), ProxyToken: runtime.proxy.Token(), Model: cfg.Inference.Model, MaximumMessageBytes: int(cfg.Inference.MaximumResponseBytes), StartTimeout: cfg.Hermes.GatewayStartTimeout})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonGatewayProtocol, err)
	}
	armed.client = runtime.hermes.Client()
	gatewaySession, err := armed.client.CreateSession(ctx, "aegis-manager")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonGatewayProtocol, err)
	}
	sessionID, err := managerdomain.NewSessionID()
	if err != nil {
		return nil, err
	}
	runtime.session, err = managerdomain.NewSession(ctx, managerdomain.SessionConfig{SessionID: sessionID, SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Route: route, Gateway: armed, GatewaySessionID: gatewaySession, Guard: guard, Operations: managerOperations{service: service, subject: subject, authority: authority}, Confirm: func(confirmCtx context.Context, preview string) (bool, error) {
		sum := sha256.Sum256([]byte(preview))
		phrase := "approve " + hex.EncodeToString(sum[:8])
		if presentation != nil {
			if emitErr := presentation.Emit(tui.Event{Kind: tui.ApprovalRequested, Origin: tui.AegisAuthoritative, Message: "deterministic credential-authority mutation approval requested"}); emitErr != nil {
				return false, emitErr
			}
			capability := presentation.State().Capabilities
			if _, writeErr := fmt.Fprintln(cmd.OutOrStdout(), tui.ApprovalCard("credential authority mutation", preview, subject.PrincipalID, managerdomain.SecurityContext, "an Aegis-controlled mutation may persist", phrase, subject.ExpiresAt, capability.Width, !capability.Unicode)); writeErr != nil {
				return false, writeErr
			}
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "[AEGIS / authoritative approval]\nOperation and exact target: %s\nAuthenticated actor: %s\nScope: built-in %s session only\nSecurity consequence: an Aegis-controlled mutation may persist in credential authority state\nAuthority expires: %s\nAllowed choices: exact phrase or cancel\nSafe default: cancel\nType exactly %q to authorize: ", preview, subject.PrincipalID, managerdomain.SecurityContext, subject.ExpiresAt.Format(time.RFC3339), phrase)
		}
		answer, eof, e := input.ReadLine(confirmCtx, int(service.Config.Manager.Ingress.MaximumMessageBytes))
		if e == nil && eof {
			e = io.EOF
		}
		approved := answer == phrase && e == nil
		if presentation != nil {
			kind, message := tui.ApprovalCancelled, "approval cancelled; no authorization granted"
			if approved {
				kind, message = tui.ApprovalConfirmed, "exact approval confirmed"
			}
			if emitErr := presentation.Emit(tui.Event{Kind: kind, Origin: tui.AegisAuthoritative, Message: message}); emitErr != nil {
				return false, emitErr
			}
		}
		return approved, e
	}, Intake: func(ctx context.Context, _ string) ([]byte, error) {
		if presentation != nil {
			if emitErr := presentation.Emit(tui.Event{Kind: tui.IntakeStarted, Origin: tui.AegisAuthoritative, Message: "protected no-echo intake started; secret content is excluded from presentation state"}); emitErr != nil {
				return nil, emitErr
			}
		}
		value, intakeErr := readSecretContext(ctx, cmd, false, "Secret value: ", "Confirm secret value: ")
		if presentation != nil {
			kind, message := tui.IntakeCompleted, "protected intake completed; content not retained"
			if intakeErr != nil {
				kind, message = tui.IntakeCancelled, "protected intake cancelled; no content retained"
			}
			if emitErr := presentation.Emit(tui.Event{Kind: kind, Origin: tui.AegisAuthoritative, Message: message}); emitErr != nil {
				for index := range value {
					value[index] = 0
				}
				return nil, emitErr
			}
		}
		return value, intakeErr
	}, Receipt: func(ctx context.Context, r managerdomain.SessionReceipt) error {
		return service.AuditManagerSession(ctx, subject, "ok", r.EndReason, map[string]string{"session_id": r.SessionID, "route_digest": r.RouteDigest, "model_digest": r.Model.Digest, "cleanup": r.Cleanup})
	}, MaximumResponseBytes: int(cfg.Hermes.MaximumResponseBytes)})
	if err != nil {
		return nil, err
	}
	go func() {
		select {
		case processErr := <-runtime.hermes.Done():
			if runtime.active.Load() {
				runtime.failures <- processErr
			}
		case <-ctx.Done():
		}
	}()
	if runtime.managed != nil {
		go func() {
			select {
			case processErr := <-runtime.managed.Done():
				if runtime.active.Load() {
					runtime.failures <- processErr
				}
			case <-ctx.Done():
			}
		}()
	}
	fail = false
	return runtime, nil
}

func (r *conversationalRuntime) Close(ctx context.Context, reason string) error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.active.Store(false)
		var joined error
		if r.testCleanup != nil {
			for _, cleanupStep := range r.testCleanup {
				joined = errors.Join(joined, cleanupStep(ctx))
			}
			cleanup := "complete"
			if joined != nil {
				cleanup = "incomplete"
			}
			if r.testFinalize != nil {
				joined = errors.Join(joined, r.testFinalize(ctx, reason, cleanup))
			}
			r.closeErr = joined
			return
		}
		if r.session != nil {
			joined = errors.Join(joined, r.cleanupStep("closing Aegis session", func() error { return r.session.Close(ctx, reason) }))
		}
		if r.hermes != nil {
			joined = errors.Join(joined, r.cleanupStep("stopping Hermes and removing disposable state", func() error { return r.hermes.Close(ctx) }))
		}
		if r.proxy != nil {
			joined = errors.Join(joined, r.cleanupStep("invalidating inference capability and closing proxy", func() error { return r.proxy.Close(ctx) }))
		}
		if r.managed == nil && r.ollama != nil && r.model != "" {
			joined = errors.Join(joined, r.cleanupStep("unloading and verifying exact model removal", func() error { return r.ollama.UnloadAndVerify(ctx, r.model) }))
		}
		if r.managed != nil {
			joined = errors.Join(joined, r.cleanupStep("stopping Aegis-managed Ollama", func() error { return r.managed.Close(ctx) }))
		}
		if r.authorityClose != nil {
			joined = errors.Join(joined, r.cleanupStep("closing credential authority", r.authorityClose))
			r.authorityClose = nil
		}
		cleanup := "complete"
		if joined != nil {
			cleanup = "incomplete"
		}
		if r.session != nil {
			joined = errors.Join(joined, r.cleanupStep("finalizing metadata-only receipt", func() error { return r.session.Finalize(ctx, reason, cleanup) }))
		}
		r.closeErr = joined
	})
	return r.closeErr
}
func (r *conversationalRuntime) cleanupStep(stage string, operation func() error) error {
	if r.cleanupEvent != nil {
		r.cleanupEvent(stage, "started")
	}
	err := operation()
	if err != nil {
		r.cleanupFailed = append(r.cleanupFailed, stage)
	}
	if r.cleanupEvent != nil {
		status := "completed"
		if err != nil {
			status = "failed"
		}
		r.cleanupEvent(stage, status)
	}
	return err
}

func (r *conversationalRuntime) cleanupFailures() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.cleanupFailed...)
}
func managerPython(installation, executable string) string {
	for _, candidate := range []string{filepath.Join(installation, "venv", "bin", "python"), filepath.Join(installation, ".venv", "bin", "python"), filepath.Join(filepath.Dir(executable), "python"), filepath.Join(filepath.Dir(executable), "python3")} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}
