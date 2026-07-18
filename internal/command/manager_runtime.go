package command

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	managerdomain "github.com/berryhill/aegis/internal/manager"
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
	client *managerdomain.GatewayClient
	budget atomic.Int32
}

func (g *armedGateway) Turn(ctx context.Context, session, text string, maximum int) ([]byte, error) {
	g.budget.Store(1)
	defer g.budget.Store(0)
	return g.client.Turn(ctx, session, text, maximum)
}
func (g *armedGateway) consume() bool { return g.budget.CompareAndSwap(1, 0) }

type conversationalRuntime struct {
	session        *managerdomain.Session
	hermes         *managerdomain.HermesProcess
	proxy          *managerdomain.Proxy
	ollama         *managerdomain.OllamaClient
	managed        *managerdomain.ManagedOllama
	model          string
	authorityClose func()
	active         atomic.Bool
}

func startConversationalManager(ctx context.Context, service *app.Service, subject core.Subject, guard *managerdomain.Guard, cmd *cobra.Command) (runtime *conversationalRuntime, err error) {
	cfg := service.Config.Manager
	if cfg.Inference.Model == "" {
		return nil, errors.New(managerdomain.ReasonNotCertified)
	}
	descriptor, err := service.Hermes.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonRuntimeUnsupported, err)
	}
	endpoint := cfg.Inference.Endpoint
	runtime = &conversationalRuntime{model: cfg.Inference.Model}
	fail := true
	defer func() {
		if fail && runtime != nil {
			_ = runtime.Close(context.Background(), "startup_failed")
		}
	}()
	if cfg.Inference.Mode == "managed" {
		runtime.managed, err = managerdomain.StartManagedOllama(ctx, cfg.Inference.Executable, service.Config.StateDir, cfg.Inference.StartTimeout)
		if err != nil {
			return nil, err
		}
		endpoint = runtime.managed.Endpoint()
	}
	runtime.ollama, err = managerdomain.NewOllamaClient(endpoint, cfg.Inference.RequestTimeout)
	if err != nil {
		return nil, err
	}
	ollamaVersion, err := runtime.ollama.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", managerdomain.ReasonOllamaUnavailable, err)
	}
	certification, err := managerdomain.LoadCertification(cfg.Inference.Certification, cfg.Inference.Model, cfg.Inference.ModelDigest, descriptor.Version, ollamaVersion, cfg.Hermes.ContextLength)
	if err != nil {
		return nil, err
	}
	if _, err = runtime.ollama.VerifyModel(ctx, cfg.Inference.Model, cfg.Inference.ModelDigest); err != nil {
		return nil, err
	}
	if err = runtime.ollama.Load(ctx, cfg.Inference.Model, cfg.Hermes.ContextLength, cfg.Inference.KeepAlive); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	route := managerdomain.RoutePlan{SchemaVersion: "aegis.manager.route.v1", ManagerID: managerdomain.LogicalAgentID, SecurityContext: managerdomain.SecurityContext, HermesPath: descriptor.Executable, HermesVersion: descriptor.Version, OllamaMode: cfg.Inference.Mode, OllamaEndpoint: endpoint, OllamaVersion: ollamaVersion, Model: certification.Identity(), ProxyIdentity: "ephemeral-session-capability", IssuedAt: now, ExpiresAt: subject.ExpiresAt}
	routeDigest, err := route.Digest()
	if err != nil {
		return nil, err
	}
	armed := &armedGateway{}
	runtime.active.Store(true)
	runtime.proxy, err = managerdomain.StartProxy(ctx, managerdomain.ProxyConfig{Target: endpoint, Model: cfg.Inference.Model, RouteDigest: routeDigest, MaximumRequestBytes: cfg.Inference.MaximumRequestBytes, MaximumResponseBytes: cfg.Inference.MaximumResponseBytes, Timeout: cfg.Inference.RequestTimeout, Guard: guard, SessionActive: runtime.active.Load, CapabilityExpires: subject.ExpiresAt, ConsumeCapability: armed.consume})
	if err != nil {
		return nil, err
	}
	python := managerPython(descriptor.Installation, descriptor.Executable)
	if python == "" {
		return nil, errors.New("Hermes gateway Python executable not found")
	}
	runtime.hermes, err = managerdomain.StartHermesProcess(ctx, managerdomain.HermesProcessConfig{Python: python, Installation: descriptor.Installation, StateRoot: service.Config.StateDir, ProxyEndpoint: runtime.proxy.Endpoint(), ProxyToken: runtime.proxy.Token(), Model: cfg.Inference.Model, MaximumMessageBytes: int(cfg.Inference.MaximumResponseBytes), StartTimeout: cfg.Hermes.GatewayStartTimeout})
	if err != nil {
		return nil, err
	}
	armed.client = runtime.hermes.Client()
	gatewaySession, err := armed.client.CreateSession(ctx, "aegis-manager")
	if err != nil {
		return nil, err
	}
	authority, closeAuthority, err := openAuthorityForService(ctx, service)
	if err != nil {
		return nil, err
	}
	runtime.authorityClose = closeAuthority
	sessionID, err := managerdomain.NewSessionID()
	if err != nil {
		return nil, err
	}
	runtime.session, err = managerdomain.NewSession(ctx, managerdomain.SessionConfig{SessionID: sessionID, SubjectID: subject.ID, PrincipalID: subject.PrincipalID, Route: route, Gateway: armed, GatewaySessionID: gatewaySession, Guard: guard, Operations: managerOperations{service: service, subject: subject, authority: authority}, Confirm: func(_ context.Context, preview string) (bool, error) {
		fmt.Fprintf(cmd.OutOrStdout(), "Aegis proposal: %s\nType yes to authorize: ", preview)
		answer, e := readConfirmation(cmd.InOrStdin())
		return answer == "yes", e
	}, Intake: func(context.Context, string) ([]byte, error) {
		return readSecret(cmd, false, "Secret value: ", "Confirm secret value: ")
	}, Receipt: func(ctx context.Context, r managerdomain.SessionReceipt) error {
		return service.AuditManagerSession(ctx, subject, "ok", r.EndReason, map[string]string{"session_id": r.SessionID, "route_digest": r.RouteDigest, "model_digest": r.Model.Digest, "cleanup": r.Cleanup})
	}, MaximumResponseBytes: int(cfg.Hermes.MaximumResponseBytes)})
	if err != nil {
		return nil, err
	}
	fail = false
	return runtime, nil
}

func (r *conversationalRuntime) Close(ctx context.Context, reason string) error {
	if r == nil {
		return nil
	}
	r.active.Store(false)
	var joined error
	if r.session != nil {
		joined = errors.Join(joined, r.session.Close(ctx, reason))
	}
	if r.hermes != nil {
		joined = errors.Join(joined, r.hermes.Close(ctx))
	}
	if r.proxy != nil {
		joined = errors.Join(joined, r.proxy.Close(ctx))
	}
	if r.ollama != nil && r.model != "" {
		joined = errors.Join(joined, r.ollama.Unload(ctx, r.model))
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
	if r.authorityClose != nil {
		r.authorityClose()
		r.authorityClose = nil
	}
	return joined
}
func managerPython(installation, executable string) string {
	for _, candidate := range []string{filepath.Join(installation, "venv", "bin", "python"), filepath.Join(installation, ".venv", "bin", "python"), filepath.Join(filepath.Dir(executable), "python"), filepath.Join(filepath.Dir(executable), "python3")} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}
