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
		"what is a Hermes profile?":                            "",
		"register it":                                          "",
		"show profiles":                                        "",
	} {
		if got := localProfileIntent(input); got != want {
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
	now := time.Now().UTC()
	subject := core.Subject{ID: "subject", PrincipalID: "principal", ExpiresAt: now.Add(time.Hour)}
	token := "test-token"
	service := &Service{
		app:         &app.Service{Config: config.Config{Principal: config.Principal{User: "ignored", UID: strconv.Itoa(os.Geteuid())}}},
		now:         func() time.Time { return now },
		profileHome: func(string, string) (string, error) { return root, nil },
		sessions: map[string]session{"degraded": {
			id: "degraded", token: sha256.Sum256([]byte(token)), subject: subject,
			expires: now.Add(time.Hour), mode: "degraded", reason: "manager_model_absent",
		}},
	}

	for input, wantKind := range map[string]string{
		"show Hermes profiles":                                 "hermes_profile_inventory",
		"register the default Hermes profile on this computer": "hermes_profile_registration_prerequisites",
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
