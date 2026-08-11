package initialize

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
)

func testService(t *testing.T) *Service {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	copy := *current
	return &Service{
		Current: func() (*user.User, error) { value := copy; return &value, nil },
		LookupID: func(uid string) (*user.User, error) {
			if uid != copy.Uid {
				return nil, errors.New("unexpected uid")
			}
			value := copy
			return &value, nil
		},
	}
}

func TestPlanUsesConfiguredPrincipalAuthorityDefault(t *testing.T) {
	root := t.TempDir()
	plan, err := testService(t).Plan(filepath.Join(root, "aegis.yaml"), filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Principal.AuthTTL != config.Defaults().Principal.AuthTTL || !strings.Contains(string(plan.Document), "auth_ttl: "+plan.Principal.AuthTTL.String()+"\n") {
		t.Fatalf("planned authority lifetime=%s document=%q", plan.Principal.AuthTTL, plan.Document)
	}
}

func TestCancelledApplyLeavesNoAcceptedPartialConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "aegis.yaml")
	service := testService(t)
	plan, err := service.Plan(path, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = service.Apply(ctx, plan); !errors.Is(err, context.Canceled) {
		t.Fatalf("apply error=%v", err)
	}
	inspection := config.Inspect(path)
	if inspection.State != config.StateAbsent {
		t.Fatalf("cancelled apply state=%s error=%v", inspection.State, inspection.Err)
	}
}

func TestApplyRefusesConfigurationCreatedAfterPreview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "aegis.yaml")
	service := testService(t)
	plan, err := service.Plan(path, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("not the planned configuration\n")
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err = service.Apply(context.Background(), plan); err == nil {
		t.Fatal("configuration created after preview was overwritten")
	}
	contents, readErr := os.ReadFile(path)
	if readErr != nil || string(contents) != string(original) {
		t.Fatalf("configuration changed: %q %v", contents, readErr)
	}
}

func TestApplyRefusesAuthorityCollisionCreatedAfterPreview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "aegis.yaml")
	state := filepath.Join(root, "state")
	service := testService(t)
	plan, err := service.Plan(path, state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(state, "mandates"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(state, "mandates", "retained.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = service.Apply(context.Background(), plan); err == nil {
		t.Fatal("authority collision created after preview was accepted")
	}
	if got := config.Inspect(path); got.State != config.StateAbsent {
		t.Fatalf("denied apply wrote configuration: %+v", got)
	}
	if _, err = os.Stat(plan.AuthorityPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denied apply created authority persistence: %v", err)
	}
}

func TestApplyPublishesSecureLayoutAndResumesEmptyAuthorityGeneration(t *testing.T) {
	root := filepath.Join(t.TempDir(), "profile")
	path := filepath.Join(root, "aegis.yaml")
	state := filepath.Join(root, "state")
	service := testService(t)
	plan, err := service.Plan(path, state)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err = ensureAuthorityPersistence(context.Background(), plan.AuthorityPath); err != nil {
		t.Fatal(err)
	}
	if err = service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if got := config.Inspect(path); got.State != config.StateValid {
		t.Fatalf("published configuration=%+v", got)
	}
	for _, directory := range []string{root, state, filepath.Dir(plan.AuthorityPath), plan.AuthorityPath} {
		info, statErr := os.Stat(directory)
		if statErr != nil {
			t.Errorf("stat %s: %v", directory, statErr)
			continue
		}
		if info.Mode().Perm()&0077 != 0 {
			t.Errorf("directory %s mode=%#o", directory, info.Mode().Perm())
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("configuration mode=%#o", info.Mode().Perm())
	}
	tokenInfo, err := os.Stat(plan.TokenPath)
	if err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(plan.TokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if tokenInfo.Mode().Perm() != 0600 || len(strings.TrimSpace(string(token))) != 64 {
		t.Fatalf("transport token mode=%#o encoded_bytes=%d", tokenInfo.Mode().Perm(), len(strings.TrimSpace(string(token))))
	}
	if strings.Contains(string(plan.Document), strings.TrimSpace(string(token))) {
		t.Fatal("reusable transport token leaked into rendered initialization document")
	}
	loaded, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.API.TokenFile != plan.TokenPath || loaded.API.UnixSocket != plan.UnixSocket || loaded.API.Token != strings.TrimSpace(string(token)) {
		t.Fatalf("initialized transport did not load through protected custody: %#v", loaded.API)
	}
}

func TestPlanRejectsUnsafeExistingTransportToken(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	tokenPath := filepath.Join(state, "transport", "api.token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := testService(t).Plan(filepath.Join(root, "aegis.yaml"), state); err == nil || !strings.Contains(err.Error(), "unsafe or ambiguous") {
		t.Fatalf("unsafe preexisting token accepted: %v", err)
	}
}

func TestApplyRevalidatesIdentityImmediatelyBeforePublication(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "aegis.yaml")
	service := testService(t)
	plan, err := service.Plan(path, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	originalCurrent := service.Current
	calls := 0
	var last *user.User
	service.Current = func() (*user.User, error) {
		calls++
		current, currentErr := originalCurrent()
		if currentErr == nil && calls >= 2 {
			current.Username += "-changed"
		}
		last = current
		return current, currentErr
	}
	service.LookupID = func(uid string) (*user.User, error) {
		if last == nil || last.Uid != uid {
			return nil, errors.New("unexpected uid")
		}
		copy := *last
		return &copy, nil
	}
	if err = service.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "changed immediately before configuration publication") {
		t.Fatalf("identity change accepted: %v", err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("denied publication wrote config: %v", err)
	}
}

func TestPlanRejectsAmbiguousHostIdentity(t *testing.T) {
	service := testService(t)
	service.LookupID = func(string) (*user.User, error) {
		return &user.User{Uid: "999999", Username: "different"}, nil
	}
	if _, err := service.Plan(filepath.Join(t.TempDir(), "aegis.yaml"), ""); err == nil {
		t.Fatal("ambiguous host identity was accepted")
	}
}

func TestApplyOperationalAuthorityAcceptsOnlyVerifiedEmptyConcurrentInitialization(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "aegis.yaml")
	service := testService(t)
	initial, err := service.Plan(configPath, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, initial.Document, 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := service.PlanOperationalAuthority(configPath)
	if err != nil {
		t.Fatal(err)
	}
	concurrent, err := authoritybadger.Initialize(context.Background(), plan.AuthorityPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.ApplyOperationalAuthority(context.Background(), plan)
	if err != nil {
		t.Fatalf("verified empty concurrent initialization was not reconciled: %v", err)
	}
	if got.GenerationID != concurrent.GenerationID || got.Digest != concurrent.Digest {
		t.Fatalf("concurrent generation identity changed: got=%+v want=%+v", got, concurrent)
	}
}

func TestApplyOperationalAuthorityConcurrentConfirmedPlansConvergeOnOneGeneration(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "aegis.yaml")
	service := testService(t)
	initial, err := service.Plan(configPath, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(configPath, initial.Document, 0600); err != nil {
		t.Fatal(err)
	}

	const workers = 16
	plans := make([]OperationalAuthorityPlan, workers)
	for index := range plans {
		plans[index], err = service.PlanOperationalAuthority(configPath)
		if err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan authoritybadger.Generation, workers)
	errors := make(chan error, workers)
	for _, plan := range plans {
		go func(plan OperationalAuthorityPlan) {
			<-start
			generation, applyErr := service.ApplyOperationalAuthority(context.Background(), plan)
			results <- generation
			errors <- applyErr
		}(plan)
	}
	close(start)

	var expected authoritybadger.Generation
	for range workers {
		generation := <-results
		if applyErr := <-errors; applyErr != nil {
			t.Fatalf("concurrent confirmed initialization failed: %v", applyErr)
		}
		if expected.GenerationID == "" {
			expected = generation
		}
		if generation.GenerationID != expected.GenerationID || generation.Digest != expected.Digest {
			t.Fatalf("concurrent plans diverged: got=%+v want=%+v", generation, expected)
		}
	}
}
