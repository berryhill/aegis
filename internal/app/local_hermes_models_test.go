package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/graph"
	"github.com/berryhill/aegis/internal/loop"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/reference"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"golang.org/x/sys/unix"
)

const modelImportConfig = `model:
  provider: openai-codex
  default: gpt-5.4
  api_key: secret-config-canary
  base_url: https://private-path-canary.invalid/api
fallback_providers:
  - provider: antigravity-oauth
    model: gemini-3-pro
    api_key: fallback-secret-canary
terminal:
  cwd: /private/workspace-canary
plugins:
  enabled: [private-plugin-canary]
`

func modelImportFixture(t *testing.T) (*Service, core.Subject, string) {
	t.Helper()
	s := testService(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(marker, []byte(modelImportConfig), 0600); err != nil {
		t.Fatal(err)
	}
	s.Config.Principal.UID = strconv.Itoa(os.Geteuid())
	s.LocalHermesHome = func(string, string) (string, error) { return root, nil }
	repo, err := fleetbadger.Open(context.Background(), filepath.Join(t.TempDir(), "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	configureLocalImportFleet(t, s, repo)
	return s, localImportPrincipal(s), marker
}

func TestLocalHermesModelImportSnapshotAndImmutableUse(t *testing.T) {
	ctx := context.Background()
	s, subject, marker := modelImportFixture(t)
	old, err := s.PrepareLocalHermesAgentImportAs(ctx, subject)
	if err != nil || old.ModelSnapshot != nil {
		t.Fatalf("legacy preview: %v", err)
	}
	p, err := s.PrepareLocalHermesModelImportAs(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if p.ModelSnapshot == nil || p.ModelSnapshot.DefaultModel != "gpt-5.4" || p.ModelSnapshot.DefaultProvider != "openai-codex" || len(p.ModelSnapshot.Lanes) != 2 || p.ModelSnapshot.Authentication != "not_connected" || p.ModelSnapshot.Qualification != "not_checked" || p.RevisionDigest == old.RevisionDigest {
		t.Fatalf("snapshot: %+v", p)
	}
	again, err := s.PrepareLocalHermesModelImportAs(ctx, subject)
	if err != nil || !reflect.DeepEqual(p, again) {
		t.Fatalf("preview drift without write: %v", err)
	}
	if _, err := s.Store.GetCharter(p.AgentID, 1); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preview wrote charter: %v", err)
	}
	if _, _, err := s.ConfirmLocalHermesAgentImportAs(ctx, subject, p.RevisionDigest); !errors.Is(err, ErrConflict) {
		t.Fatalf("legacy approval broadened: %v", err)
	}
	if _, _, err := s.ConfirmLocalHermesModelImportAs(ctx, subject, old.RevisionDigest); !errors.Is(err, ErrConflict) {
		t.Fatalf("old approval broadened: %v", err)
	}
	agent, created, err := s.ConfirmLocalHermesModelImportAs(ctx, subject, p.RevisionDigest)
	if err != nil || !created {
		t.Fatalf("confirm: %t %v", created, err)
	}
	stored, err := s.Store.GetCharter(agent.Revision.Charter.ID, 1)
	if err != nil || stored.Digest != p.CharterDigest {
		t.Fatalf("charter readback: %v", err)
	}
	h := stored.Charter.Stanzas[0].Hermes
	if h.ProviderAuthentication.Mode != core.ConfiguredProviderV1 || len(stored.Charter.Stanzas[0].Scopes.Credentials) != 0 {
		t.Fatal("configuration granted credentials")
	}
	data, _ := json.Marshal([]any{p, stored, agent})
	for _, secret := range []string{"secret-config-canary", "private-path-canary", "fallback-secret-canary", "workspace-canary", "private-plugin-canary", filepath.Dir(marker)} {
		if strings.Contains(string(data), secret) {
			t.Fatal("private profile content escaped snapshot")
		}
	}
	if err := os.WriteFile(marker, []byte(strings.ReplaceAll(modelImportConfig, "gpt-5.4", "gpt-5.5")), 0600); err != nil {
		t.Fatal(err)
	}
	// All durable consumption must work without even resolving the source root.
	s.LocalHermesHome = func(string, string) (string, error) { t.Fatal("consumed import reopened profile"); return "", nil }
	replayed, created, err := s.ConfirmLocalHermesModelImportAs(ctx, subject, p.RevisionDigest)
	if err != nil || created || replayed.Revision.Digest != agent.Revision.Digest {
		t.Fatalf("immutable replay: %t %v", created, err)
	}
	verified, err := s.VerifyLocalHermesAgentImportForBootstrapAs(ctx, localBootstrapImportPrincipal(s))
	if err != nil || verified.Revision.Digest != agent.Revision.Digest {
		t.Fatalf("bootstrap replay: %v", err)
	}
	if _, err := h.ResolveAuthorizedLane("openai-codex", "gpt-5.5"); !errors.Is(err, core.ErrUnauthorizedHermesLane) {
		t.Fatalf("mutable config changed authority: %v", err)
	}
	for _, pair := range [][2]string{{"", ""}, {"antigravity-oauth", "gemini-3-pro"}} {
		lane, err := h.ResolveAuthorizedLane(pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.resolveControllerProviderAuthentication(ctx, h.WithLane(lane)); !errors.Is(err, hermes.ErrProviderAuthUnauthorized) {
			t.Fatalf("configured means connected: %v", err)
		}
	}

	// Actual publication resolves per-step and per-node selections from the exact
	// registered charter. No test-only lane resolver or mutable profile fallback.
	makeLoop := func(id, provider, model string) loop.LoopRevision {
		r, _, err := loop.NewRevision(loop.LoopRevision{SchemaVersion: loop.RevisionSchemaVersion, LoopID: id, Revision: 1, Name: id, EntryStepID: "act", Steps: []loop.Step{{ID: "act", Kind: loop.StepAction, Provider: provider, Model: model, Retry: loop.RetryPolicy{MaxAttempts: 1}, EvidenceClaims: []loop.EvidenceClaim{{Claim: "output-present", VerifierID: "aegis.blob.present", PolicyVersion: "v1", MediaType: "text/plain", ExpectedDigest: "sha256:" + strings.Repeat("a", 64)}}}, {ID: "done", Kind: loop.StepTerminal, Retry: loop.RetryPolicy{MaxAttempts: 1}, Terminal: &loop.TerminalDefinition{Outcome: loop.OutcomeSucceeded}}}, Transitions: []loop.Transition{{ID: "t1", FromStepID: "act", ToStepID: "done"}}, RequiredEvidence: []loop.EvidenceRequirement{{Claim: "output-present", ProducerStepID: "act"}}})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, tc := range []struct {
		id, provider, model string
		denied              bool
	}{{"default", "", "", false}, {"alternate", "antigravity-oauth", "gemini-3-pro", false}, {"edited", "openai-codex", "gpt-5.5", true}} {
		r := makeLoop("snapshot-"+tc.id, tc.provider, tc.model)
		_, err := s.PublishLoopAs(ctx, subject, PublishLoopInput{AgentID: agent.Revision.AgentID, Revision: r, IdempotencyKey: "publish-" + tc.id})
		if tc.denied {
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("step outside snapshot: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("step lane publication: %v", err)
		}
	}
	base := makeLoop("snapshot-default", "", "")
	ref := reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: base.LoopID, Revision: 1, Digest: base.Digest}
	if _, err := s.SetLoopLifecycleAs(ctx, subject, base.LoopID, SetLoopLifecycleInput{AgentID: agent.Revision.AgentID, Loop: ref, State: loop.LifecycleActive, EventID: "activate-snapshot"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id, provider, model string
		denied              bool
	}{{"default", "", "", false}, {"alternate", "antigravity-oauth", "gemini-3-pro", false}, {"edited", "openai-codex", "gpt-5.5", true}} {
		g, _, err := graph.NewRevision(graph.GraphRevision{SchemaVersion: graph.RevisionSchemaVersion, GraphID: "snapshot-graph-" + tc.id, Revision: 1, Nodes: []graph.Node{{ID: "node", Loop: ref, Participant: agentRevisionRef(agent.Revision), Provider: tc.provider, Model: tc.model}}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.PublishGraphAs(ctx, subject, PublishGraphInput{AgentID: agent.Revision.AgentID, Revision: g, IdempotencyKey: "graph-" + tc.id})
		if tc.denied {
			if !errors.Is(err, ErrDenied) {
				t.Fatalf("node outside snapshot: %v", err)
			}
		} else if err != nil {
			t.Fatalf("node lane publication: %v", err)
		}
	}
}

func TestLocalHermesModelImportRecoversOrphanAndPreservesLifecycle(t *testing.T) {
	s, subject, _ := modelImportFixture(t)
	ctx := context.Background()
	p, err := s.PrepareLocalHermesModelImportAs(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	repository := &failOnceAgentRegistrationRepository{Repository: s.FleetRepository, fail: true}
	configureLocalImportFleet(t, s, repository)
	if _, _, err := s.ConfirmLocalHermesModelImportAs(ctx, subject, p.RevisionDigest); err == nil {
		t.Fatal("injected registration failure lost")
	}
	stored, err := s.Store.GetCharter(p.AgentID, 1)
	if err != nil || stored.Digest != p.CharterDigest {
		t.Fatalf("orphan charter: %v", err)
	}
	agent, created, err := s.ConfirmLocalHermesModelImportAs(ctx, subject, p.RevisionDigest)
	if err != nil || !created {
		t.Fatalf("recover exact orphan: %v", err)
	}
	input, err := NewSetAgentLifecycleInput(p.AgentID, 1, agent.Revision.Digest, "disabled")
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := s.SetAgentLifecycleAs(ctx, subject, p.AgentID, input)
	if err != nil {
		t.Fatal(err)
	}
	s.LocalHermesHome = func(string, string) (string, error) { t.Fatal("historical replay opened profile"); return "", nil }
	if _, created, err := s.ConfirmLocalHermesModelImportAs(ctx, subject, p.RevisionDigest); err != nil || created {
		t.Fatalf("replay: %v", err)
	}
	latest, err := s.VerifyLocalHermesAgentImportForBootstrapAs(ctx, localBootstrapImportPrincipal(s))
	if err != nil || latest.Revision.Digest != disabled.Revision.Digest {
		t.Fatalf("replay rewrote lifecycle: %v", err)
	}
}

func TestLocalHermesModelImportDoesNotMigrateHistoricalRegistration(t *testing.T) {
	s, subject, _ := modelImportFixture(t)
	ctx := context.Background()
	old, err := s.PrepareLocalHermesAgentImportAs(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ConfirmLocalHermesAgentImportAs(ctx, subject, old.RevisionDigest); err != nil {
		t.Fatal(err)
	}
	p, err := s.PrepareLocalHermesModelImportAs(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ConfirmLocalHermesModelImportAs(ctx, subject, p.RevisionDigest); !errors.Is(err, ErrConflict) {
		t.Fatalf("historical registration migrated: %v", err)
	}
	stored, err := s.Store.GetCharter(old.AgentID, 1)
	if err != nil || stored.Digest != old.CharterDigest || stored.Charter.Stanzas[0].Hermes.Model != "none" {
		t.Fatalf("historical authority changed: %v", err)
	}
}

func TestLocalHermesModelImportUnconsumedDrift(t *testing.T) {
	for _, change := range []string{"selection", "ignored-secret", "replace"} {
		t.Run(change, func(t *testing.T) {
			s, subject, marker := modelImportFixture(t)
			p, err := s.PrepareLocalHermesModelImportAs(context.Background(), subject)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.Stat(marker)
			data := strings.ReplaceAll(modelImportConfig, "gpt-5.4", "gpt-5.5")
			if change == "ignored-secret" {
				data = strings.ReplaceAll(modelImportConfig, "secret-config-canary", "secret-change-canary")
			}
			if change == "replace" {
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				data = modelImportConfig
			}
			if err := os.WriteFile(marker, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(marker, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.ConfirmLocalHermesModelImportAs(context.Background(), subject, p.RevisionDigest); !errors.Is(err, ErrConflict) {
				t.Fatalf("drift accepted: %v", err)
			}
			if _, err := s.Store.GetCharter(p.AgentID, 1); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("drift persisted charter")
			}
		})
	}
}

func TestLocalHermesModelImportRejectsUnsafeConfiguration(t *testing.T) {
	bad := []string{
		"model: {provider: openai-codex, provider: antigravity-oauth, default: gpt-5.4}",
		"model: {provider: openai-codex, default: gpt-5.4}\nsecret: {key: one, key: two}",
		"model: &model {provider: openai-codex, default: gpt-5.4}\ncopy: *model",
		"model: {provider: openai-codex, default: gpt-5.4}\n---\npassword: source-canary",
		"model: {provider: unknown-secret-canary, default: gpt-5.4}",
		"model: {provider: openai-codex, default: /private/source-canary}",
		"model: {provider: openai-codex, default: '${PRIVATE_SECRET}'}",
		"model: {provider: openai-codex, default: sk-source-canary}",
		"model: {provider: openai-codex, default: true}",
		"model: {provider: openai-codex, default: gpt-5.4}\nfallback_providers: [{provider: openai-codex, model: gpt-5.4}]",
		"model: {provider: openai-codex, default: gpt-5.4}\nfallback_providers: [{provider: auto, model: gemini-3-pro}]",
		"model: [source-canary", "model: gpt-5.4", "{}", "",
		strings.Repeat(" ", maximumLocalHermesMarkerBytes+1),
		"model: {provider: openai-codex, default: gpt-5.4}\nx: " + strings.Repeat("[", 35) + "0" + strings.Repeat("]", 35),
	}
	for i, input := range bad {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if _, err := parseLocalHermesModels([]byte(input)); err != errLocalHermesModels {
				t.Fatalf("unsafe config error = %v", err)
			}
		})
	}
	for _, provider := range []string{"codex", "openai-codex", "antigravity-oauth"} {
		if _, err := parseLocalHermesModels([]byte("model: {provider: " + provider + ", default: gpt-5.4}")); err != nil {
			t.Fatalf("configured metadata rejected for unqualified transport: %v", err)
		}
	}
}

func TestLocalHermesModelImportOwnerAndFileDenials(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "writable", "directory", "fifo", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			s, subject, marker := modelImportFixture(t)
			switch kind {
			case "symlink":
				if err := os.Rename(marker, marker+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(marker+".saved", marker); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(marker, marker+".link"); err != nil {
					t.Fatal(err)
				}
			case "writable":
				if err := os.Chmod(marker, 0666); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(marker, 0700); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				if err := unix.Mkfifo(marker, 0600); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				if err := os.Truncate(marker, maximumLocalHermesMarkerBytes+1); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.PrepareLocalHermesModelImportAs(context.Background(), subject); err != errLocalHermesModels {
				t.Fatalf("unsafe marker: %v", err)
			}
		})
	}
	s, subject, _ := modelImportFixture(t)
	s.LocalHermesHome = func(string, string) (string, error) { t.Fatal("unauthorized profile read"); return "", nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.PrepareLocalHermesModelImportAs(ctx, subject); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preview: %v", err)
	}
	subject.PrincipalID = "foreign"
	if _, err := s.PrepareLocalHermesModelImportAs(context.Background(), subject); !errors.Is(err, ErrDenied) {
		t.Fatalf("foreign principal: %v", err)
	}
}
