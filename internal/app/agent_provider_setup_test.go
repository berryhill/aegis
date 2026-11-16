package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/registry"
	"github.com/berryhill/aegis/internal/runtime/hermes"
)

// Controlled metadata fixture ONLY. Native parser/qualification remains covered
// by the existing Hermes tests; this fixture makes no live qualification claim.
type providerSetupSource struct {
	account, entry string
	ambiguous      bool
}

func (f *providerSetupSource) InspectNativeCodexCandidates(_ context.Context, home string, _ time.Time) (hermes.NativeCodexCandidates, error) {
	return hermes.NativeCodexCandidates{ProfileHome: home, DistinctEligibleGrants: 2, Candidates: []hermes.NativeCodexCandidate{{CredentialID: f.entry, Eligible: true, Reason: "eligible"}}}, nil
}
func (f *providerSetupSource) PreviewNativeCodexLogin(_ context.Context, _ string, _ time.Time, ids ...string) (hermes.NativeCodexMetadata, error) {
	if len(ids) != 1 || (ids[0] == "" && f.ambiguous) || (ids[0] != "" && ids[0] != f.entry) {
		return hermes.NativeCodexMetadata{}, hermes.ErrProviderAuthInvalid
	}
	return hermes.NativeCodexMetadata{AccountID: f.account}, nil
}
func setupProviderFixture(t *testing.T, models bool) (*Service, core.Subject, string, string, *providerSetupSource, FleetAgent) {
	t.Helper()
	s := testService(t)
	s.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	sub := localImportPrincipal(s)
	repo, err := fleetbadger.Open(context.Background(), filepath.Join(t.TempDir(), "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	// This fixture tests definition/connection services, not installed Doer
	// preparation. Keep constructor wiring hermetic, then restore the real
	// synthetic adapter for unchanged runtime qualification-denial assertions.
	adapter := s.Hermes
	s.Hermes = nil
	configureLocalImportFleet(t, s, repo)
	s.Hermes = adapter
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	synthetic := *u
	synthetic.HomeDir = root
	s.Config.Principal.User = u.Username
	s.Current = func() (*user.User, error) { return &synthetic, nil }
	home := filepath.Join(root, ".hermes")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(marker, []byte(modelImportConfig), 0600); err != nil {
		t.Fatal(err)
	}
	s.LocalHermesHome = func(string, string) (string, error) { return home, nil }
	s.ConfigFile = filepath.Join(root, "owner.json")
	raw, _ := json.Marshal(s.Config)
	if err := os.WriteFile(s.ConfigFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	source := &providerSetupSource{account: "synthetic-account", entry: "nc-" + strings.Repeat("a", 64), ambiguous: true}
	s.nativeCodexMetadata = source
	var p LocalHermesAgentImportProposal
	if models {
		p, err = s.PrepareLocalHermesModelImportAs(context.Background(), sub)
	} else {
		p, err = s.PrepareLocalHermesAgentImportAs(context.Background(), sub)
	}
	if err != nil {
		t.Fatal(err)
	}
	var agent FleetAgent
	if models {
		agent, _, err = s.ConfirmLocalHermesModelImportAs(context.Background(), sub, p.RevisionDigest)
	} else {
		agent, _, err = s.ConfirmLocalHermesAgentImportAs(context.Background(), sub, p.RevisionDigest)
	}
	if err != nil {
		t.Fatal(err)
	}
	return s, sub, marker, home, source, agent
}
func providerCall(t *testing.T, s *Service, sub core.Subject, id string, in AgentProviderSetupInput) AgentProviderSetupResult {
	t.Helper()
	out, err := s.AgentProviderSetupAs(context.Background(), sub, id, in)
	if err != nil {
		t.Fatalf("%s: %v", in.Action, err)
	}
	return out
}
func providerConnect(t *testing.T, s *Service, sub core.Subject, id, home, entry string) {
	t.Helper()
	review := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "review", ProfileHome: home, CredentialID: entry})
	connected := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "connect", ReviewID: review.Connection.ReviewID})
	if connected.Connection.Status != "connected" || connected.Executable {
		t.Fatal("connection overclaimed")
	}
}
func TestAgentProviderSetupComposition(t *testing.T) {
	s, sub, marker, home, source, initial := setupProviderFixture(t, true)
	id := initial.Revision.AgentID
	configBefore, _ := os.ReadFile(s.ConfigFile)
	profileBefore, _ := os.ReadFile(marker)
	status := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "status"})
	if status.Connection.Status != "not_connected" || status.Executable {
		t.Fatal("registration granted connection")
	}
	candidates := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "candidates", ProfileHome: "default"})
	if candidates.Candidates.DistinctEligibleGrants != 2 {
		t.Fatal("candidate inspection lost ambiguity")
	}
	if _, err := s.AgentProviderSetupAs(context.Background(), sub, id, AgentProviderSetupInput{Action: "review", ProfileHome: home}); err == nil {
		t.Fatal("ambiguous selection admitted")
	}
	providerConnect(t, s, sub, id, home, source.entry)
	current, _ := s.GetFleetAgentAs(context.Background(), sub, id, 0)
	if !reflect.DeepEqual(current, initial) {
		t.Fatal("connection changed Agent")
	}
	p := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "promote", Provider: "openai-codex", Model: "gpt-5.4"})
	if p.Review == nil || p.Review.Expected != agentRevisionRef(initial.Revision) || p.Review.Charter.Charter.Revision != 2 {
		t.Fatal("exact successor missing")
	}
	if _, err := s.Store.GetCharter(id, 2); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview mutated charter")
	}
	if p.Review.Connection == nil || p.Review.Connection.CredentialID != source.entry || p.Review.Connection.AccountID != source.account {
		t.Fatal("successor review omitted exact account/entry")
	}
	// Caller cannot mutate held review through response pointers.
	digest := p.Review.Charter.Digest
	p.Review.Charter.Charter.Stanzas[0].Hermes.Model = "gpt-9-injected"
	input := AgentProviderSetupInput{Action: "approve", ReviewID: p.Review.ID, CharterDigest: digest}
	wrong := input
	wrong.CharterDigest = initial.Revision.Digest
	if _, err := s.AgentProviderSetupAs(context.Background(), sub, id, wrong); err == nil {
		t.Fatal("registration digest approved successor")
	}
	applied := providerCall(t, s, sub, id, input)
	if applied.Agent.Revision.Revision != 2 || applied.Agent.Revision.Charter.Digest != digest || applied.Models.Model != "gpt-5.4" || applied.Models.ProviderAuthentication.Mode != core.ControllerCodexAccessTokenV1 {
		t.Fatal("incorrect promoted successor")
	}
	old, _ := s.Store.GetCharter(id, 1)
	next, _ := s.Store.GetCharter(id, 2)
	expected := old.Charter
	expected.Revision = 2
	expected.Stanzas[0].Hermes.ProviderAuthentication = &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}
	for i := range expected.Stanzas[0].Hermes.AllowedLanes {
		l := &expected.Stanzas[0].Hermes.AllowedLanes[i]
		if l.Provider == "openai-codex" && l.Model == "gpt-5.4" {
			l.ProviderAuthentication = &core.ProviderAuthentication{Mode: core.ControllerCodexAccessTokenV1}
		}
	}
	want, err := core.Canonicalize(expected)
	if err != nil || want.Digest != next.Digest || len(next.Charter.Stanzas[0].Scopes.Credentials) != 0 {
		t.Fatal("unreviewed fields changed")
	}
	h := next.Charter.Stanzas[0].Hermes
	verifyPromotedLanePublication(t, s, sub, applied.Agent)
	for _, pair := range [][2]string{{"", ""}, {"openai-codex", "gpt-5.4"}} {
		l, err := h.ResolveAuthorizedLane(pair[0], pair[1])
		if err != nil || l.ProviderAuthentication.Mode != core.ControllerCodexAccessTokenV1 {
			t.Fatal("immutable default/override missing")
		}
	}
	alternate, err := h.ResolveAuthorizedLane("antigravity-oauth", "gemini-3-pro")
	if err != nil || alternate.ProviderAuthentication.Mode != core.ConfiguredProviderV1 {
		t.Fatal("non-Codex lane changed")
	}
	if _, err = s.resolveControllerProviderAuthentication(context.Background(), h.WithLane(alternate)); !errors.Is(err, hermes.ErrProviderAuthUnauthorized) {
		t.Fatal("configured lane executable")
	}
	// Real unqualified runtime still denies; metadata fixture is not its resolver.
	if _, err = s.resolveControllerProviderAuthentication(context.Background(), h); !errors.Is(err, hermes.ErrProviderAuthRuntimeUnqualified) {
		t.Fatalf("qualification bypass: %v", err)
	}
	source.account = "later-drift"
	s.LocalHermesHome = func(string, string) (string, error) { t.Fatal("consumed replay reopened profile"); return "", nil }
	replay := providerCall(t, s, sub, id, input)
	if replay.Agent.Revision.Digest != applied.Agent.Revision.Digest {
		t.Fatal("replay changed result")
	}
	if _, created, err := s.ConfirmLocalHermesModelImportAs(context.Background(), sub, initial.Revision.Digest); err != nil || created {
		t.Fatal("initial consumed replay changed")
	}
	afterConfig, _ := os.ReadFile(s.ConfigFile)
	afterProfile, _ := os.ReadFile(marker)
	if !bytes.Equal(configBefore, afterConfig) || !bytes.Equal(profileBefore, afterProfile) {
		t.Fatal("source/config mutated")
	}
	raw, _ := json.Marshal([]any{status, candidates, applied})
	for _, canary := range []string{"secret-config-canary", "fallback-secret-canary", "private-plugin-canary"} {
		if bytes.Contains(raw, []byte(canary)) {
			t.Fatal("secret escaped")
		}
	}
}

func TestAgentProviderSetupMigrationAndConcurrentReplay(t *testing.T) {
	s, sub, _, _, _, initial := setupProviderFixture(t, false)
	id := initial.Revision.AgentID
	p := providerCall(t, s, sub, id, AgentProviderSetupInput{Action: "models"})
	input := AgentProviderSetupInput{Action: "approve", ReviewID: p.Review.ID, CharterDigest: p.Review.Charter.Digest}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, err := s.AgentProviderSetupAs(context.Background(), sub, id, input); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	history, err := s.ListFleetAgentRevisionsAs(context.Background(), sub, id)
	if err != nil || len(history) != 2 {
		t.Fatalf("duplicate effects: %d %v", len(history), err)
	}
	old, _ := s.Store.GetCharter(id, 1)
	next, _ := s.Store.GetCharter(id, 2)
	if old.Charter.Stanzas[0].Hermes.Model != "none" || next.Charter.Stanzas[0].Hermes.ProviderAuthentication.Mode != core.ConfiguredProviderV1 {
		t.Fatal("migration rewrote history or connected")
	}
	agents, err := s.ListFleetAgentsAs(context.Background(), sub)
	if err != nil || len(agents) != 1 {
		t.Fatal("migration duplicated identity")
	}
}

func verifyPromotedLanePublication(t *testing.T, s *Service, sub core.Subject, agent FleetAgent) {
	t.Helper()
	ctx := context.Background()
	for _, tc := range []struct {
		id, provider, model string
		denied              bool
	}{
		{"default", "", "", false}, {"codex", "openai-codex", "gpt-5.4", false}, {"alternate", "antigravity-oauth", "gemini-3-pro", false}, {"unapproved", "openai-codex", "gpt-5.5", true},
	} {
		r, _, err := loop.NewRevision(loop.LoopRevision{SchemaVersion: loop.RevisionSchemaVersion, LoopID: "promoted-" + tc.id, Revision: 1, Name: "promoted", EntryStepID: "act", Steps: []loop.Step{
			{ID: "act", Kind: loop.StepAction, Provider: tc.provider, Model: tc.model, Retry: loop.RetryPolicy{MaxAttempts: 1}, EvidenceClaims: []loop.EvidenceClaim{{Claim: "output-present", VerifierID: "aegis.blob.present", PolicyVersion: "v1", MediaType: "text/plain", ExpectedDigest: "sha256:" + strings.Repeat("a", 64)}}},
			{ID: "done", Kind: loop.StepTerminal, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}},
		}, Transitions: []loop.Transition{{ID: "t1", FromStepID: "act", ToStepID: "done"}}, RequiredEvidence: []loop.EvidenceRequirement{{Claim: "output-present", ProducerStepID: "act"}}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.PublishLoopAs(ctx, sub, PublishLoopInput{AgentID: agent.Revision.AgentID, Revision: r, IdempotencyKey: "publish-promoted-" + tc.id})
		if tc.denied {
			if !errors.Is(err, ErrDenied) {
				t.Fatal("unapproved step admitted", err)
			}
			continue
		}
		if err != nil {
			t.Fatal("promoted step publication", err)
		}
		ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: r.LoopID, Revision: 1, Digest: r.Digest}
		if _, err = s.SetLoopLifecycleAs(ctx, sub, r.LoopID, SetLoopLifecycleInput{AgentID: agent.Revision.AgentID, Loop: ref, State: loop.LifecycleActive, EventID: "activate-promoted-" + tc.id}); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []bool{false, true} {
			provider, model, suffix := tc.provider, tc.model, "good"
			if bad {
				provider, model, suffix = "openai-codex", "gpt-5.5", "bad"
			}
			g, _, err := graph.NewRevision(graph.GraphRevision{SchemaVersion: graph.RevisionSchemaVersion, GraphID: "promoted-" + tc.id + "-" + suffix, Revision: 1, Nodes: []graph.Node{{ID: "node", Loop: ref, Participant: agentRevisionRef(agent.Revision), Provider: provider, Model: model}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.PublishGraphAs(ctx, sub, PublishGraphInput{AgentID: agent.Revision.AgentID, Revision: g, IdempotencyKey: g.GraphID})
			if bad {
				if !errors.Is(err, ErrDenied) {
					t.Fatal("unapproved node admitted", err)
				}
			} else if err != nil {
				t.Fatal("promoted node publication", err)
			}
		}
	}
}

type setupFailPublish struct {
	fleet.Repository
	fail bool
}

func (r *setupFailPublish) PublishAgentRevision(ctx context.Context, revision registry.AgentRevision, fact fleet.AuditFact, admission ...func() error) error {
	if r.fail {
		r.fail = false
		return errors.New("synthetic append failure")
	}
	return r.Repository.PublishAgentRevision(ctx, revision, fact, admission...)
}
func TestAgentProviderSetupReviewDenialsAndRecovery(t *testing.T) {
	for _, kind := range []string{"profile-drift", "config-drift", "account-drift", "entry-drift", "agent-drift", "charter-collision", "foreign", "manager", "expiry", "partial"} {
		t.Run(kind, func(t *testing.T) {
			models := kind != "profile-drift"
			s, sub, marker, home, source, initial := setupProviderFixture(t, models)
			id := initial.Revision.AgentID
			in := AgentProviderSetupInput{Action: "models"}
			if models {
				providerConnect(t, s, sub, id, home, source.entry)
				in = AgentProviderSetupInput{Action: "promote", Provider: "openai-codex", Model: "gpt-5.4"}
			}
			p := providerCall(t, s, sub, id, in)
			input := AgentProviderSetupInput{Action: "approve", ReviewID: p.Review.ID, CharterDigest: p.Review.Charter.Digest}
			switch kind {
			case "profile-drift":
				os.WriteFile(marker, []byte(strings.ReplaceAll(modelImportConfig, "gpt-5.4", "gpt-5.5")), 0600)
			case "config-drift":
				f, err := os.OpenFile(s.ConfigFile, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				f.WriteString("\n ")
				f.Close()
			case "account-drift":
				source.account = "changed-account"
			case "entry-drift":
				source.entry = "nc-" + strings.Repeat("b", 64)
			case "agent-drift":
				_, err := s.SetAgentLifecycleAs(context.Background(), sub, id, SetAgentLifecycleInput{Expected: agentRevisionRef(initial.Revision), Lifecycle: registry.LifecycleDisabled})
				if err != nil {
					t.Fatal(err)
				}
			case "charter-collision":
				c := p.Review.Charter.Charter
				c.Name = "different approved proposal"
				other, err := core.Canonicalize(c)
				if err != nil {
					t.Fatal(err)
				}
				if err = s.Store.SaveCharter(other); err != nil {
					t.Fatal(err)
				}
			case "foreign":
				sub.PrincipalID = "foreign"
			case "manager":
				sub.Kind = "agent"
			case "expiry":
				now := s.Now().Add(6 * time.Minute)
				s.Now = func() time.Time { return now }
				sub.AuthenticatedAt = now
				sub.ExpiresAt = now.Add(time.Minute)
			case "partial":
				s.FleetRepository = &setupFailPublish{Repository: s.FleetRepository, fail: true}
			}
			if _, err := s.AgentProviderSetupAs(context.Background(), sub, id, input); err == nil {
				t.Fatal("denial admitted")
			}
			if kind == "partial" {
				out := providerCall(t, s, sub, id, input)
				if out.Agent.Revision.Revision != 2 {
					t.Fatal("orphan recovery duplicated")
				}
			}
		})
	}
}
