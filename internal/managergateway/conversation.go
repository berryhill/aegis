package managergateway

import (
	"context"

	"errors"
	"fmt"
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
)

type gatewayOperations struct {
	service *app.Service
	subject core.Subject
}

func (o gatewayOperations) authority() (*credentials.Authority, error) {
	if o.service.CredentialAuthority == nil {
		return nil, errors.New(managerdomain.ReasonAuthorityUnavailable)
	}
	return o.service.CredentialAuthority, nil
}

func (o gatewayOperations) Status(context.Context) (map[string]any, error) {
	return map[string]any{"manager": managerdomain.LogicalAgentID, "context": managerdomain.SecurityContext, "route": "authenticated-unix-gateway"}, nil
}
func (o gatewayOperations) List(ctx context.Context, q string, n int) ([]credentials.SecretRecord, error) {
	authority, err := o.authority()
	if err != nil {
		return nil, err
	}
	return authority.List(ctx, q, n)
}
func (o gatewayOperations) Counts(ctx context.Context) (credentials.SecretCounts, error) {
	authority, err := o.authority()
	if err != nil {
		return credentials.SecretCounts{}, err
	}
	return authority.Counts(ctx)
}
func (o gatewayOperations) ReadValue(ctx context.Context, reference string, consume func(credentials.SecretRecord, []byte) error) error {
	authority, err := o.authority()
	if err != nil {
		return err
	}
	return authority.ReadValue(ctx, reference, func(record credentials.SecretRecord, value []byte) error {
		if err := o.service.AuditCredentialOperation(ctx, o.subject, "credential_value_read", "ok", "authenticated_gateway_manager_read_value", record.ID); err != nil {
			return err
		}
		return consume(record, value)
	})
}
func (o gatewayOperations) Metadata(ctx context.Context, id string) (credentials.SecretRecord, error) {
	authority, err := o.authority()
	if err != nil {
		return credentials.SecretRecord{}, err
	}
	return authority.Metadata(ctx, id)
}
func (o gatewayOperations) History(ctx context.Context, id string, limit int) ([]credentials.SecretVersionMetadata, error) {
	authority, err := o.authority()
	if err != nil {
		return nil, err
	}
	return authority.History(ctx, id, limit)
}
func (o gatewayOperations) Create(context.Context, managerdomain.CreateArguments, []byte) (credentials.SecretRecord, error) {
	return credentials.SecretRecord{}, errors.New("interactive approval and protected intake are unavailable through the gateway turn endpoint")
}
func (o gatewayOperations) Rotate(context.Context, managerdomain.RotateArguments, []byte) (credentials.SecretRecord, error) {
	return credentials.SecretRecord{}, errors.New("interactive approval and protected intake are unavailable through the gateway turn endpoint")
}
func (o gatewayOperations) Revoke(context.Context, managerdomain.RevokeArguments) error {
	return errors.New("interactive approval is unavailable through the gateway turn endpoint")
}
func (o gatewayOperations) Bind(context.Context, managerdomain.BindingArguments) error {
	return errors.New("interactive approval is unavailable through the gateway turn endpoint")
}
func (o gatewayOperations) VerifyAudit(ctx context.Context) error { return o.service.VerifyAudit(ctx) }

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

type conversation struct {
	session   *managerdomain.Session
	hermes    *managerdomain.HermesProcess
	proxy     *managerdomain.Proxy
	ollama    *managerdomain.OllamaClient
	managed   *managerdomain.ManagedOllama
	model     string
	ctx       context.Context
	cancel    context.CancelFunc
	turnMu    sync.Mutex
	active    atomic.Bool
	failures  chan error
	closeOnce sync.Once
	closeErr  error
}

func startConversation(startupCtx, lifetimeCtx context.Context, service *app.Service, subject core.Subject, managerSessionID string, mandateExpires time.Time) (runtime *conversation, err error) {
	cfg := service.Config.Manager
	if cfg.Inference.Model == "" {
		return nil, errors.New(managerdomain.ReasonModelAbsent)
	}
	if service.CredentialAuthority == nil {
		return nil, errors.New(managerdomain.ReasonAuthorityUnavailable)
	}
	if cfg.Hermes.ContextLength < 65536 {
		return nil, errors.New(managerdomain.ReasonContextUnsupported)
	}
	descriptor, err := service.Hermes.Discover(startupCtx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonRuntimeUnsupported, err)
	}
	endpoint := cfg.Inference.Endpoint
	runtimeCtx, cancelRuntime := context.WithDeadline(lifetimeCtx, mandateExpires)
	runtime = &conversation{model: cfg.Inference.Model, ctx: runtimeCtx, cancel: cancelRuntime, failures: make(chan error, 1)}
	failed := true
	defer func() {
		if failed && runtime != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), cfg.CleanupTimeout)
			defer cancel()
			err = errors.Join(err, runtime.Close(cleanup, managerdomain.EndStartupFailed))
		}
	}()
	if cfg.Inference.Mode == "managed" {
		runtime.managed, err = managerdomain.StartManagedOllama(startupCtx, cfg.Inference.Executable, service.Config.StateDir, cfg.Inference.StartTimeout)
		if err != nil {
			return nil, err
		}
		endpoint = runtime.managed.Endpoint()
	}
	runtime.ollama, err = managerdomain.NewOllamaClient(endpoint, cfg.Inference.RequestTimeout)
	if err != nil {
		return nil, err
	}
	ollamaVersion, err := runtime.ollama.Version(startupCtx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonOllamaUnavailable, err)
	}
	if _, err = runtime.ollama.VerifyModel(startupCtx, cfg.Inference.Model, cfg.Inference.ModelDigest); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (!strings.Contains(err.Error(), managerdomain.ReasonModelAbsent) && !strings.Contains(err.Error(), managerdomain.ReasonDigestMismatch)) {
			return nil, fmt.Errorf("%s: %w", managerdomain.ReasonOllamaUnavailable, err)
		}
		return nil, err
	}
	certification, err := managerdomain.LoadCertification(cfg.Inference.Certification, cfg.Inference.Model, cfg.Inference.ModelDigest, descriptor.Version, ollamaVersion, cfg.Hermes.ContextLength)
	if err != nil {
		return nil, err
	}
	if err = runtime.ollama.Load(startupCtx, cfg.Inference.Model, cfg.Hermes.ContextLength, cfg.Inference.KeepAlive); err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonModelLoadFailed, err)
	}
	now := time.Now().UTC()
	route := managerdomain.RoutePlan{SchemaVersion: "aegis.manager.route.v1", ManagerID: managerdomain.LogicalAgentID, SecurityContext: managerdomain.SecurityContext, HermesPath: descriptor.Executable, HermesVersion: descriptor.Version, OllamaMode: cfg.Inference.Mode, OllamaEndpoint: endpoint, OllamaVersion: ollamaVersion, Model: certification.Identity(), ProxyIdentity: "linux-pidfd-process-custody", IssuedAt: now, ExpiresAt: mandateExpires}
	routeDigest, err := route.Digest()
	if err != nil {
		return nil, err
	}
	guard, err := managerdomain.NewGuard(int(cfg.Ingress.MaximumMessageBytes), cfg.Ingress.MaximumMessageRunes, cfg.Ingress.BoundedDecodeDepth, cfg.Ingress.ScanTimeout)
	if err != nil {
		return nil, err
	}
	sensitive := &managerdomain.SensitiveTracker{}
	armed := &armedGateway{sensitive: sensitive}
	processAuthorizer := managerdomain.NewProcessAuthorizer()
	runtime.active.Store(true)
	runtime.proxy, err = managerdomain.StartProxy(runtime.ctx, managerdomain.ProxyConfig{Target: endpoint, Model: cfg.Inference.Model, RouteDigest: routeDigest, MaximumRequestBytes: cfg.Inference.MaximumRequestBytes, MaximumResponseBytes: cfg.Inference.MaximumResponseBytes, Timeout: cfg.Inference.RequestTimeout, Guard: guard, SessionActive: runtime.active.Load, ProcessAuthorizer: processAuthorizer, CapabilityExpires: mandateExpires, ConsumeCapability: armed.consume, RequireSystemInstruction: true, AllowPlaintextRequests: true, Sensitive: sensitive})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonRouteMismatch, err)
	}
	python := gatewayManagerPython(descriptor.Installation, descriptor.Executable)
	if python == "" {
		return nil, errors.New(managerdomain.ReasonRuntimeUnsupported + ": Hermes gateway Python executable not found")
	}
	runtime.hermes, err = managerdomain.StartHermesProcess(startupCtx, managerdomain.HermesProcessConfig{Python: python, Installation: descriptor.Installation, StateRoot: service.Config.StateDir, ProxyEndpoint: runtime.proxy.Endpoint(), Model: cfg.Inference.Model, MaximumMessageBytes: int(cfg.Inference.MaximumResponseBytes), StartTimeout: cfg.Hermes.GatewayStartTimeout, AuthorizeRelease: processAuthorizer.Bind})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonGatewayProtocol, err)
	}
	armed.client = runtime.hermes.Client()
	gatewaySession, err := armed.client.CreateSession(startupCtx, "aegis-manager")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonGatewayProtocol, err)
	}
	runtime.session, err = managerdomain.NewSession(runtime.ctx, managerdomain.SessionConfig{
		SessionID: managerSessionID, SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Route: route, Gateway: armed, GatewaySessionID: gatewaySession,
		Guard: guard, Operations: gatewayOperations{service: service, subject: subject},
		Confirm: func(context.Context, string) (bool, error) { return false, nil },
		Intake: func(context.Context, string) ([]byte, error) {
			return nil, errors.New("protected intake requires an interactive Aegis-owned terminal boundary")
		},
		Receipt: func(ctx context.Context, receipt managerdomain.SessionReceipt) error {
			return service.AuditManagerSession(ctx, subject, "ok", receipt.EndReason, map[string]string{"session_id": receipt.SessionID, "route_digest": receipt.RouteDigest, "model_digest": receipt.Model.Digest, "cleanup": receipt.Cleanup})
		}, MaximumResponseBytes: int(cfg.Hermes.MaximumResponseBytes),
	})
	if err != nil {
		return nil, err
	}
	go runtime.watchProcess(runtime.ctx, runtime.hermes.Done())
	if runtime.managed != nil {
		go runtime.watchProcess(runtime.ctx, runtime.managed.Done())
	}
	failed = false
	return runtime, nil
}

func (r *conversation) watchProcess(ctx context.Context, done <-chan error) {
	select {
	case err := <-done:
		if r.active.Load() {
			select {
			case r.failures <- err:
			default:
			}
		}
	case <-ctx.Done():
	}
}

func bindTurnContext(requestCtx, runtimeCtx context.Context) (context.Context, context.CancelFunc) {
	if runtimeCtx == nil {
		runtimeCtx = context.Background()
	}
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(requestCtx)
	stopRuntime := context.AfterFunc(runtimeCtx, cancel)
	return ctx, func() {
		stopRuntime()
		cancel()
	}
}

func (r *conversation) Turn(ctx context.Context, input string) (string, error) {
	if r == nil || r.session == nil || !r.active.Load() {
		return "", errors.New("conversational_runtime_unavailable")
	}
	r.turnMu.Lock()
	defer r.turnMu.Unlock()
	turnCtx, release := bindTurnContext(ctx, r.ctx)
	defer release()
	if err := turnCtx.Err(); err != nil {
		return "", err
	}
	return r.session.Handle(turnCtx, input)
}

func (r *conversation) Close(ctx context.Context, reason string) error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.active.Store(false)
		if r.cancel != nil {
			r.cancel()
		}
		var joined error
		if r.session != nil {
			joined = errors.Join(joined, r.session.Close(ctx, reason))
		}
		if r.proxy != nil {
			joined = errors.Join(joined, r.proxy.Close(ctx))
		}
		if r.managed == nil && r.ollama != nil && r.model != "" {
			joined = errors.Join(joined, r.ollama.UnloadAndVerify(ctx, r.model))
		}
		if r.hermes != nil {
			joined = errors.Join(joined, r.hermes.Close(ctx))
		}
		if r.managed != nil {
			joined = errors.Join(joined, r.managed.Close(ctx))
		}
		cleanup := "complete"
		if joined != nil {
			cleanup = "incomplete"
		}
		if r.session != nil {
			joined = errors.Join(joined, r.session.Finalize(ctx, reason, cleanup))
		}
		r.closeErr = joined
	})
	return r.closeErr
}

func gatewayManagerPython(installation, executable string) string {
	for _, candidate := range []string{filepath.Join(installation, "venv", "bin", "python"), filepath.Join(installation, ".venv", "bin", "python"), filepath.Join(filepath.Dir(executable), "python"), filepath.Join(filepath.Dir(executable), "python3")} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}
