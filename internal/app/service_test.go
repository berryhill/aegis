package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
)

type auditFixture struct {
	events []core.AuditEvent
}

func (a *auditFixture) AppendAudit(_ context.Context, event core.AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}
func (a *auditFixture) AuditEvents() ([]core.AuditEvent, error) {
	return append([]core.AuditEvent(nil), a.events...), nil
}
func (a *auditFixture) VerifyAudit() error { return nil }

func testCharter(now time.Time) core.Charter {
	base := func(id string) core.TrustStanza {
		return core.TrustStanza{ID: id, Name: id, Enabled: true, Authentication: core.AuthenticationPolicy{Methods: []string{"local-os"}, Selectors: []core.IdentitySelector{{Kinds: []string{"human"}}}}, Grant: core.Grant{Capabilities: []string{"chat"}, Tools: []string{"no_mcp"}}, Session: core.SessionPolicy{MaximumLifetimeSec: 60}, Approval: core.ApprovalPolicy{MaximumLifetimeSec: 60, SingleUse: true}, InformationFlow: core.InformationFlowPolicy{CrossStanza: "deny"}, Scopes: core.Scopes{Credentials: []string{"provider:test"}}, Hermes: core.HermesConfig{Toolsets: []string{"no_mcp"}, Model: "test-model", Provider: "test"}}
	}
	principal := base("principal")
	principal.Authentication.Selectors = []core.IdentitySelector{{SubjectIDs: []string{"local-uid:4242"}, PrincipalIDs: []string{"principal-1"}, Issuers: []string{"local-os"}, Environments: []string{"local"}}}
	principal.Scopes = core.Scopes{Memory: []string{"principal-memory"}, Credentials: []string{"provider:test"}}
	team := base("teamwide")
	team.Authentication.Selectors = []core.IdentitySelector{{SubjectIDs: []string{"team-user"}, Issuers: []string{"local-os"}, Environments: []string{"local"}}}
	team.Grant.Tools = []string{"web"}
	team.Hermes.Toolsets = []string{"web"}
	team.Scopes = core.Scopes{Memory: []string{"team-memory"}, Credentials: []string{"provider:test"}}
	return core.Charter{SchemaVersion: core.SchemaVersion, AgentID: "office", Name: "Office", Revision: 1, Runtime: core.RuntimeConstraint{Adapter: "hermes", Runtime: "hermes-agent", VersionConstraint: ">=0.18.0,<0.19.0", Target: "aegis-owned-ephemeral"}, Stanzas: []core.TrustStanza{principal, team}, CreatedBy: "principal-1", CreatedAt: now}
}
func testService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	exe := filepath.Join(root, "hermes-test")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'Hermes Agent v0.18.2'; echo 'Install directory: /isolated/test'; exit 0; fi\nprintf '%s\\n' \"$@\" > \"$HERMES_HOME/launch-args\"\nprintf '%s' \"${TEST_PROVIDER_KEY:-}\" > \"$HERMES_HOME/provider-present\"\nsleep 30 &\necho $! > \"$HERMES_HOME/child.pid\"\nwait\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.StateDir = st.Root()
	cfg.HermesExecutable = exe
	cfg.Principal = config.Principal{ID: "principal-1", Name: "Principal Operator", UID: "4242", User: "operator", AuthTTL: 5 * time.Minute}
	cfg.Credentials.ProviderAuth["test"] = config.CredentialBinding{Type: "environment", SourceEnv: "AEGIS_TEST_PROVIDER_KEY", TargetEnv: "TEST_PROVIDER_KEY"}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(cfg, st, hermes.New(exe, log), log)
	s.Now = func() time.Time { return time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC) }
	s.Current = func() (*user.User, error) { return &user.User{Uid: "4242", Username: "operator"}, nil }
	s.LookupEnv = func(name string) (string, bool) {
		if name == "AEGIS_TEST_PROVIDER_KEY" {
			return "principal-provider-secret", true
		}
		return "", false
	}
	return s
}

func TestAuthenticationDoesNotAcceptPromptOrStanzaAuthority(t *testing.T) {
	s := testService(t)
	s.Current = func() (*user.User, error) { return &user.User{Uid: "2000", Username: "attacker"}, nil }
	subject, err := s.Authenticate(context.Background())
	if err != nil || subject.PrincipalID != "" {
		t.Fatalf("non-principal OS authentication=%+v err=%v", subject, err)
	}
	if err = s.requirePrincipal(subject); !errors.Is(err, ErrDenied) {
		t.Fatalf("mismatched account gained principal authority: %v", err)
	}
	cc, _ := core.Canonicalize(testCharter(s.Now()))
	sub := core.Subject{ID: "attacker", Kind: "human", Issuer: "prompt", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
	if d, err := s.Select(cc, sub, "principal", core.Environment{Name: "local"}); err == nil || d.Allowed {
		t.Fatal("requested stanza or prompt identity bypassed authorization")
	}
}

func TestCredentialResolutionIsExplicitScopedAndSecretSafe(t *testing.T) {
	s := testService(t)
	s.Config.Credentials.ProviderAuth["team"] = config.CredentialBinding{Type: "environment", SourceEnv: "AEGIS_TEAM_KEY", TargetEnv: "TEAM_PROVIDER_KEY"}
	s.LookupEnv = func(name string) (string, bool) {
		values := map[string]string{"AEGIS_TEST_PROVIDER_KEY": "principal-secret-sentinel", "AEGIS_TEAM_KEY": "team-secret-sentinel"}
		value, ok := values[name]
		return value, ok
	}
	credentials, err := s.resolveProviderCredential("team", []string{"provider:team"})
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || credentials[0].Reference != "provider:team" || credentials[0].TargetEnv != "TEAM_PROVIDER_KEY" || credentials[0].Value != "team-secret-sentinel" {
		t.Fatalf("wrong credential resolved: %#v", credentials)
	}
	if _, err = s.resolveProviderCredential("team", []string{"provider:test"}); err == nil {
		t.Fatal("credential outside selected stanza scope was resolved")
	}
	s.LookupEnv = func(string) (string, bool) { return "principal-secret-sentinel", false }
	_, err = s.resolveProviderCredential("team", []string{"provider:team"})
	if err == nil || strings.Contains(err.Error(), "principal-secret-sentinel") {
		t.Fatalf("missing credential error leaked a value or was accepted: %v", err)
	}
}

func TestSelectionZeroAmbiguousAndNoUnion(t *testing.T) {
	s := testService(t)
	c := testCharter(s.Now())
	cc, _ := core.Canonicalize(c)
	unknown := core.Subject{ID: "none", Kind: "service", Issuer: "local-os", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
	if _, err := s.Select(cc, unknown, "", core.Environment{Name: "local"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("zero match=%v", err)
	}
	c.Stanzas[1].Authentication.Selectors = []core.IdentitySelector{{PrincipalIDs: []string{"principal-1"}, Issuers: []string{"local-os"}, Environments: []string{"local"}}}
	cc = core.CanonicalCharter{Charter: c, Digest: "sha256:ambiguous-test"}
	principalSubject := core.Subject{ID: "local-uid:4242", Kind: "human", PrincipalID: "principal-1", Issuer: "local-os", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
	d, err := s.Select(cc, principalSubject, "", core.Environment{Name: "local"})
	if !errors.Is(err, ErrAmbiguous) || d.Selected != nil {
		t.Fatalf("ambiguous selection=%+v err=%v", d, err)
	}
	d, err = s.Select(cc, principalSubject, "principal", core.Environment{Name: "local"})
	if err != nil || len(d.Selected.Grant.Tools) != 1 || d.Selected.Grant.Tools[0] != "no_mcp" {
		t.Fatalf("authority was not exactly one stanza: %+v %v", d, err)
	}
}

func TestCharterRejectsDeterminableSelectorOverlap(t *testing.T) {
	tests := []struct {
		name string
		edit func(*core.Charter)
	}{
		{"principal", func(c *core.Charter) {
			c.Stanzas[1].Authentication.Selectors = []core.IdentitySelector{{PrincipalIDs: []string{"principal-1"}}}
		}},
		{"subject", func(c *core.Charter) {
			c.Stanzas[0].Authentication.Selectors = []core.IdentitySelector{{SubjectIDs: []string{"same"}}}
			c.Stanzas[1].Authentication.Selectors = []core.IdentitySelector{{SubjectIDs: []string{"same"}}}
		}},
		{"issuer and claim", func(c *core.Charter) {
			c.Stanzas[0].Authentication.Selectors = []core.IdentitySelector{{Issuers: []string{"issuer"}, Claims: map[string]string{"team": "a"}}}
			c.Stanzas[1].Authentication.Selectors = []core.IdentitySelector{{Issuers: []string{"issuer"}, Claims: map[string]string{"team": "a"}}}
		}},
		{"environment wildcard intersection", func(c *core.Charter) {
			c.Stanzas[0].Authentication.Selectors = []core.IdentitySelector{{Kinds: []string{"human"}}}
			c.Stanzas[1].Authentication.Selectors = []core.IdentitySelector{{Kinds: []string{"human"}, Environments: []string{"local"}}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := testCharter(time.Now().UTC())
			tt.edit(&c)
			if err := core.ValidateCharter(c); err == nil {
				t.Fatal("overlapping selectors accepted")
			}
		})
	}
}

func TestApprovalExactSingleUseAndMutation(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	cc, _ := core.Canonicalize(testCharter(s.Now()))
	if err := s.Store.SaveCharter(cc); err != nil {
		t.Fatal(err)
	}
	review, err := s.PreviewPlan(ctx, "office", 1, core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	if review.CharterDigest != cc.Digest || review.PlanDigest != review.Plan.Digest {
		t.Fatalf("review digests are not explicit: %+v", review)
	}
	if got := review.RequestedToolsets["principal"]; len(got) != 1 || got[0] != "no_mcp" {
		t.Fatalf("review toolsets=%v", got)
	}
	if got := review.CredentialScopes["principal"]; len(got) != 1 || got[0] != "provider:test" {
		t.Fatalf("review credentials=%v", got)
	}
	if got := review.MemoryScopes["principal"]; len(got) != 1 || got[0] != "principal-memory" {
		t.Fatalf("review memory=%v", got)
	}
	if policy := review.ApprovalRequirements["principal"]; !policy.SingleUse || policy.MaximumLifetimeSec != 60 {
		t.Fatalf("review approval semantics=%+v", policy)
	}
	a, err := s.RequestApproval(ctx, review.Plan.ID, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	a, err = s.DecideApproval(ctx, a.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	mut := review.Plan
	mut.Digest = "sha256:mutated"
	if err = s.Store.Save("plans", "mutated", mut); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Apply(ctx, "mutated", a.ID); err == nil {
		t.Fatal("mutated plan consumed approval")
	}
	r, err := s.Apply(ctx, review.Plan.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "verified" {
		t.Fatalf("receipt=%+v", r)
	}
	if _, err = s.Apply(ctx, review.Plan.ID, a.ID); err == nil {
		t.Fatal("consumed approval replay succeeded")
	}
}

func TestApprovalReviewContainsFullPreviousRevisionDiff(t *testing.T) {
	now := time.Now().UTC()
	first, err := core.Canonicalize(testCharter(now))
	if err != nil {
		t.Fatal(err)
	}
	secondCharter := first.Charter
	secondCharter.Revision = 2
	secondCharter.Name = "Office Revised"
	second, err := core.Canonicalize(secondCharter)
	if err != nil {
		t.Fatal(err)
	}
	diff := fullCharterDiff(&first, second)
	for _, required := range []string{"--- office revision 1", "+++ office revision 2", `-  "name": "Office"`, `+  "name": "Office Revised"`, `-  "revision": 1`, `+  "revision": 2`} {
		if !strings.Contains(diff, required) {
			t.Fatalf("full charter diff omitted %q:\n%s", required, diff)
		}
	}
}

func TestStoredPlanDigestBindsEveryProtectedField(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	canonical, err := core.Canonicalize(testCharter(s.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	review, err := s.PreviewPlan(ctx, "office", 1, core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name string
		edit func(*core.Plan)
	}{
		{"id", func(p *core.Plan) { p.ID = "changed" }},
		{"agent", func(p *core.Plan) { p.AgentID = "changed" }},
		{"revision", func(p *core.Plan) { p.Revision++ }},
		{"charter digest", func(p *core.Plan) { p.CharterDigest = "sha256:changed" }},
		{"runtime identity", func(p *core.Plan) { p.Runtime.Runtime = "changed" }},
		{"runtime version", func(p *core.Plan) { p.Runtime.Version = "0.18.3" }},
		{"runtime executable", func(p *core.Plan) { p.Runtime.Executable = "/changed" }},
		{"adapter version", func(p *core.Plan) { p.Runtime.AdapterVersion = "changed" }},
		{"environment", func(p *core.Plan) { p.Environment.Name = "changed" }},
		{"effect kind", func(p *core.Plan) { p.Effects[0].Kind = core.EffectModifyFile }},
		{"effect target", func(p *core.Plan) { p.Effects[0].Target += ".changed" }},
		{"effect digest", func(p *core.Plan) { p.Effects[0].Digest = "sha256:changed" }},
		{"effect consequence", func(p *core.Plan) { p.Effects[0].Consequential = false }},
		{"creation time", func(p *core.Plan) { p.CreatedAt = p.CreatedAt.Add(time.Second) }},
	}
	for i, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			plan := review.Plan
			plan.Effects = append([]core.Effect(nil), review.Plan.Effects...)
			mutation.edit(&plan)
			storageID := fmt.Sprintf("mutated-%d", i)
			if err := s.Store.Save("plans", storageID, plan); err != nil {
				t.Fatal(err)
			}
			if _, err := s.GetPlan(storageID); !errors.Is(err, ErrConflict) {
				t.Fatalf("mutation was not rejected: %v", err)
			}
		})
	}
}

func TestProvisioningEffectClassesAreExplicitAndDefaultDeny(t *testing.T) {
	for _, kind := range []string{core.EffectModifyFile, core.EffectCreateHermesProfile, core.EffectConfigureMCP, core.EffectConfigurePlugin, core.EffectStartGateway, core.EffectInstallService, core.EffectCreateCron, core.EffectExternalNetwork} {
		supported, err := supportedProvisioningEffect(kind)
		if err != nil || supported {
			t.Fatalf("unsupported effect %q classification supported=%v err=%v", kind, supported, err)
		}
	}
	if supported, err := supportedProvisioningEffect(core.EffectCreateFile); err != nil || !supported {
		t.Fatalf("create-file classification supported=%v err=%v", supported, err)
	}
	if _, err := supportedProvisioningEffect("model_invented_effect"); err == nil {
		t.Fatal("unknown effect class was not denied")
	}
}

func TestAuditInspectionRequiresPrincipal(t *testing.T) {
	s := testService(t)
	nonPrincipal := core.Subject{ID: "local-uid:2000", Kind: "human", Issuer: "local-os", Method: "local-os", AuthenticatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}
	if _, err := s.AuditEventsAs(nonPrincipal); !errors.Is(err, ErrDenied) {
		t.Fatalf("non-principal audit read error=%v", err)
	}
	if err := s.VerifyAuditAs(nonPrincipal); !errors.Is(err, ErrDenied) {
		t.Fatalf("non-principal audit verification error=%v", err)
	}
}

func TestAuditAuthorityIsAnInjectableNarrowBoundary(t *testing.T) {
	s := testService(t)
	fixture := &auditFixture{}
	s.Audit = fixture
	if _, err := s.Authenticate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fixture.events) != 1 || fixture.events[0].Type != "authentication" {
		t.Fatalf("injected audit authority events=%+v", fixture.events)
	}
	stored, err := s.Store.AuditEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 0 {
		t.Fatalf("application bypassed injected audit authority: %+v", stored)
	}
}

func TestCharterValidationEmitsAuthoritativeAuditEvent(t *testing.T) {
	s := testService(t)
	fixture := &auditFixture{}
	s.Audit = fixture
	charter := testCharter(s.Now())
	data, err := json.Marshal(charter)
	if err != nil {
		t.Fatal(err)
	}
	subject := core.Subject{ID: "team-user", Kind: "human", Issuer: "local-os", Method: "local-os", AuthenticatedAt: s.Now(), ExpiresAt: s.Now().Add(time.Minute)}
	canonical, err := s.ValidateCharterAs(context.Background(), subject, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.events) != 1 {
		t.Fatalf("audit events=%+v", fixture.events)
	}
	event := fixture.events[0]
	if event.Type != "charter" || event.Reason != "charter_validated" || event.SubjectID != subject.ID || event.AgentID != charter.AgentID || event.Runtime != "hermes-agent" || event.CharterDigest != canonical.Digest {
		t.Fatalf("validation audit event=%+v", event)
	}
}

func TestInterruptedProvisioningRecoveryRollsBackMatchingArtifact(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	canonical, err := core.Canonicalize(testCharter(s.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.SaveCharter(canonical); err != nil {
		t.Fatal(err)
	}
	review, err := s.PreviewPlan(ctx, "office", 1, core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"agent_id": canonical.Charter.AgentID, "revision": canonical.Charter.Revision, "charter_digest": canonical.Digest, "runtime": canonical.Charter.Runtime, "stanzas": canonical.Charter.Stanzas}
	target := review.Plan.Effects[0].Target
	if err = s.Store.PublishProvisioned(target, payload); err != nil {
		t.Fatal(err)
	}
	receipt := core.Receipt{ID: "interrupted", PlanID: review.Plan.ID, ApprovalID: "approval", CharterDigest: canonical.Digest, Status: "provisioning", StartedAt: s.Now()}
	if err = s.Store.Save("receipts", receipt.ID, receipt); err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverProvisioning(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("matching interrupted artifact was not removed: %v", err)
	}
	recovered, err := s.GetReceipt(receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "failed" || recovered.FinishedAt.IsZero() || recovered.Failure != "interrupted provisioning recovered" {
		t.Fatalf("recovered receipt=%+v", recovered)
	}
}

func TestExpiredApprovalCannotBeUsed(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	now := s.Now()
	s.Now = func() time.Time { return now }
	cc, _ := core.Canonicalize(testCharter(now))
	if err := s.Store.SaveCharter(cc); err != nil {
		t.Fatal(err)
	}
	review, err := s.PreviewPlan(ctx, "office", 1, core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.RequestApproval(ctx, review.Plan.ID, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	a, err = s.DecideApproval(ctx, a.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	if _, err = s.Apply(ctx, review.Plan.ID, a.ID); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired approval apply error=%v", err)
	}
}

func TestDesignSessionCannotProvision(t *testing.T) {
	s := testService(t)
	if _, err := s.DesignSmoke(context.Background(), nil); err == nil {
		t.Fatal("empty unrelated draft was accepted as a design proposal")
	}
	if _, err := os.Stat(filepath.Join(s.Store.Root(), "provisioned")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("design created provisioning artifacts: %v", err)
	}
}

func TestCleanSessionsAndRevocation(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	c := testCharter(s.Now())
	cc, _ := core.Canonicalize(c)
	if err := s.Store.SaveCharter(cc); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.Save("receipts", "ready", core.Receipt{ID: "ready", CharterDigest: cc.Digest, Status: "verified"}); err != nil {
		t.Fatal(err)
	}
	m1, _, err := s.PreviewSession(ctx, "office", 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	x1, err := s.StartSession(ctx, m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if x1.ToolsetVerification != "launch_arguments" || len(x1.VerifiedToolsets) != 1 || x1.VerifiedToolsets[0] != "no_mcp" {
		t.Fatalf("launched toolset verification=%+v", x1)
	}
	arguments, err := os.ReadFile(filepath.Join(x1.RuntimeHome, "launch-args"))
	if err != nil || !strings.Contains(string(arguments), "--toolsets\nno_mcp\n") {
		t.Fatalf("actual Hermes launch arguments do not contain approved toolset: %q %v", arguments, err)
	}
	provider, err := os.ReadFile(filepath.Join(x1.RuntimeHome, "provider-present"))
	if err != nil || string(provider) != "principal-provider-secret" {
		t.Fatalf("selected provider credential did not reach launch: %v", err)
	}
	m2, _, err := s.PreviewSession(ctx, "office", 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	x2, err := s.StartSession(ctx, m2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if x1.RuntimeHome == x2.RuntimeHome || x1.RuntimeSessionID == x2.RuntimeSessionID {
		t.Fatal("runtime context reused")
	}
	if err = s.RevokeSession(ctx, x1.ID, "test"); err != nil {
		t.Fatal(err)
	}
	_, alive, err := s.InspectSession(x1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if alive {
		t.Fatal("runtime remained alive after revocation")
	}
	childBytes, err := os.ReadFile(filepath.Join(x1.RuntimeHome, "child.pid"))
	if err == nil {
		childPID, _ := strconv.Atoi(strings.TrimSpace(string(childBytes)))
		deadline := time.Now().Add(2 * time.Second)
		for processAlive(childPID) && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if processAlive(childPID) {
			t.Fatal("runtime child process remained alive after revocation")
		}
	}
	_ = s.TerminateSession(ctx, x2.ID, "cleanup")
}
func TestAuditTamperingDetectedAndSecretsRedacted(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if err := s.Store.AppendAudit(ctx, core.AuditEvent{Type: "test", Outcome: "ok", Reason: "test", Metadata: map[string]string{"token": "raw-secret"}}); err != nil {
		t.Fatal(err)
	}
	es, err := s.Store.AuditEvents()
	if err != nil {
		t.Fatal(err)
	}
	if es[0].Metadata["token"] != "[REDACTED]" {
		t.Fatal("secret not redacted")
	}
	path := filepath.Join(s.Store.Root(), "audit.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var e core.AuditEvent
	if err = json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	e.Outcome = "tampered"
	b, _ = json.Marshal(e)
	if err = os.WriteFile(path, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.VerifyAudit(); err == nil {
		t.Fatal("audit tampering was not detected")
	}
}
