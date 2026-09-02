package managergateway

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
)

func TestDiscoverHermesProfilesReturnsSanitizedDeterministicInventory(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.MkdirAll(filepath.Join(root, "profiles", "zeta"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "profiles", "alpha"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "profiles", "forged\x1bname"), 0o700); err != nil {
		t.Fatal(err)
	}
	profiles, err := discoverHermesProfiles(root, strconv.Itoa(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 3 || profiles[0].ID != "default" || profiles[1].ID != "alpha" || profiles[2].ID != "zeta" {
		t.Fatalf("profiles=%+v", profiles)
	}
	for _, profile := range profiles {
		if profile.Status != "discovered" || strings.Contains(profile.Target, root) {
			t.Fatalf("unsafe or invalid descriptor: %+v", profile)
		}
	}
}

func TestDiscoverHermesProfilesRejectsSymlinkedNamedProfileWithoutFollowingIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.MkdirAll(filepath.Join(root, "profiles"), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "profiles", "forged")); err != nil {
		t.Fatal(err)
	}
	profiles, err := discoverHermesProfiles(root, strconv.Itoa(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profiles[1].ID != "forged" || profiles[1].Status != "rejected" || profiles[1].Reason == "" {
		t.Fatalf("symlink was not surfaced as rejected: %+v", profiles)
	}
}

func TestDiscoverHermesProfilesFailsClosedAboveInventoryBound(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".hermes")
	namedRoot := filepath.Join(root, "profiles")
	if err := os.MkdirAll(namedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= maxHermesNamedProfiles; index++ {
		if err := os.Mkdir(filepath.Join(namedRoot, fmt.Sprintf("profile-%03d", index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := discoverHermesProfiles(root, strconv.Itoa(os.Geteuid())); err == nil || !strings.Contains(err.Error(), "inventory exceeds limit") {
		t.Fatalf("oversized profile inventory must fail closed, got %v", err)
	}
}

func TestLocalProfileIntentIsClosedAndDoesNotTreatQuestionsAsMutation(t *testing.T) {
	for input, want := range map[string]string{
		"can you see all the hermes profiles on this computer?": "inventory",
		"show Hermes profiles":                                 "inventory",
		"register the default Hermes profile on this computer": "register_default",
		"import the local Hermes default profile as an Agent":  "register_default",
		"i want to register an agent":                          "register_default",
		"Can you register an agent?":                           "register_default",
		"hey, let's register an agent":                         "register_default",
		"okay please let's register a new agent":               "register_default",
		"what is a Hermes profile?":                            "",
		"register it":                                          "",
		"show profiles":                                        "",
		"do not import the local Hermes default profile":       "",
		"how do I import the local Hermes default profile?":    "",
		"say 'import the local Hermes default profile'":        "",
		"hey,\nlet's register an agent":                        "",
	} {
		if got := localProfileIntent(input); got != want {
			t.Fatalf("input=%q got=%q want=%q", input, got, want)
		}
	}
}

func TestAgentRegistryIntentIsClosedAndComplete(t *testing.T) {
	for input, want := range map[string]agentRegistryIntent{
		"how many agents have we registered?":           {kind: "count"},
		"which agents are registered?":                  {kind: "list"},
		"show agent agent-alpha":                        {kind: "show", agentID: "agent-alpha"},
		"show details for agent agent-alpha revision 2": {kind: "show", agentID: "agent-alpha", revision: 2, hasRevision: true},
		"how do I list registered agents?":              {},
		"do not list registered agents":                 {},
		"say 'list registered agents'":                  {},
		"show it":                                       {},
		"show agent":                                    {},
		"list agents and register another one":          {},
		"how many agents?\nconfirm it":                  {},
	} {
		if got := parseAgentRegistryIntent(input); got != want {
			t.Fatalf("input=%q got=%+v want=%+v", input, got, want)
		}
	}
}

func TestPlatformGuidanceIntentRecognizesCompleteSelfExpertiseQuestions(t *testing.T) {
	for input, want := range map[string]string{
		"how do I install the Aegis skills in Hermes?":                 "skills_install",
		"does our registered Hermes agent know how to use Aegis?":      "registered_agent_expertise",
		"does our registers hermes agent know how to use aegis/":       "registered_agent_expertise",
		"can you change the name of the default Hermes agent to javi?": "agent_rename",
		"rename it": "",
		"say 'install the Aegis skills in Hermes'":                   "",
		"do not change the name of the default Hermes agent to javi": "",
		"please quote 'install the Aegis skills in Hermes'":          "",
		"review this sentence: use Aegis skills in Hermes":           "",
		"what do you call the agent?":                                "",
		"why did you rename the agent?":                              "",
	} {
		if got := platformGuidanceIntent(input); got != want {
			t.Fatalf("input=%q got=%q want=%q", input, got, want)
		}
	}
}

func TestRegistrationGuidanceNeverClaimsMutation(t *testing.T) {
	profiles := []HermesProfileDescriptor{{ID: "default", Kind: "default", Target: "profile/default", Status: "discovered"}}
	message := renderProfileInventory(profiles) + "\n\nRegistration was not performed."
	if !strings.Contains(message, "read-only") || !strings.Contains(message, "not performed") {
		t.Fatalf("guidance lacks authority boundary: %q", message)
	}
}

func TestDegradedManagerTurnServesAuthoritativeProfileIntentsWithoutModel(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".hermes")
	if err := os.MkdirAll(filepath.Join(root, "profiles", "alpha"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	uid := strconv.Itoa(os.Geteuid())
	subject := core.Subject{ID: "local-uid:" + uid, PrincipalID: "principal", Issuer: "linux-so-peercred", Method: "local-os", AuthenticatedAt: now, ExpiresAt: now.Add(time.Hour), Claims: map[string]string{"uid": uid}}
	token := "test-token"
	service := &Service{
		app: &app.Service{
			Config:          config.Config{Principal: config.Principal{ID: "principal", User: "ignored", UID: uid, AuthTTL: time.Hour}},
			Now:             func() time.Time { return now },
			LocalHermesHome: func(string, string) (string, error) { return root, nil },
		},
		now:         func() time.Time { return now },
		profileHome: func(string, string) (string, error) { return root, nil },
		sessions: map[string]session{"degraded": {
			id: "degraded", token: sha256.Sum256([]byte(token)), subject: subject,
			expires: now.Add(time.Hour), mode: "degraded", reason: "manager_model_absent",
		}},
	}

	for input, wantKind := range map[string]string{
		"show Hermes profiles":                                 "hermes_profile_inventory",
		"register the default Hermes profile on this computer": "local_hermes_agent_import_prepared",
		"i want to register an agent":                          "local_hermes_agent_import_prepared",
	} {
		result, err := service.Turn(context.Background(), subject, "degraded", token, input)
		if err != nil {
			t.Fatalf("authoritative degraded turn %q: %v", input, err)
		}
		if result.Kind != wantKind || result.Origin != TurnOriginAuthoritative || result.Data["model_bypassed"] != true {
			t.Fatalf("unexpected authoritative degraded result: %+v", result)
		}
	}

	if _, err := service.Turn(context.Background(), subject, "degraded", token, "hello"); !errors.Is(err, ErrTurnRuntimeUnavailable) {
		t.Fatalf("generic degraded turn must remain unavailable, got %v", err)
	}
}
