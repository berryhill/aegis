package reset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/berryhill/aegis/internal/initialize"
)

type fixture struct {
	home, config, state, checkpoints string
	service                          *Service
	current                          *user.User
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	copyUser := *current
	service := &Service{
		Current: func() (*user.User, error) { value := copyUser; return &value, nil },
		LookupID: func(uid string) (*user.User, error) {
			if uid != copyUser.Uid {
				return nil, errors.New("unknown uid")
			}
			value := copyUser
			return &value, nil
		},
		HomeDir: func() (string, error) { return home, nil },
	}
	return fixture{home: home, config: filepath.Join(home, ".config", "aegis", "aegis.yaml"), state: filepath.Join(home, "custom-state"), checkpoints: filepath.Join(home, "custom-checkpoints"), service: service, current: current}
}

func (f fixture) writeConfig(t *testing.T, extra string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.config), 0700); err != nil {
		t.Fatal(err)
	}
	document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\n%s", f.state, f.current.Uid, f.current.Username, f.checkpoints, extra)
	if err := os.WriteFile(f.config, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
}

func writeOwned(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteResetAndFirstRunReplay(t *testing.T) {
	f := newFixture(t)
	f.writeConfig(t, "")
	paths := []string{
		filepath.Join(filepath.Dir(f.config), ".aegis-model-config-interrupted.yaml"),
		filepath.Join(f.state, ".lock"),
		filepath.Join(f.state, "audit.jsonl"),
		filepath.Join(f.state, "plans", "plan-one.json"),
		filepath.Join(f.state, "approvals", "approval-one.json"),
		filepath.Join(f.state, "receipts", "receipt-one.json"),
		filepath.Join(f.state, "mandates", "mandate-one.json"),
		filepath.Join(f.state, "sessions", "session-one.json"),
		filepath.Join(f.state, "charters", "agent", "one.json"),
		filepath.Join(f.state, "provisioned", "agent", "1", "mapping.json"),
		filepath.Join(f.state, "runtime", "manager-123", "session.json"),
		filepath.Join(f.state, "manager", "certifications", "candidate.json"),
		filepath.Join(f.checkpoints, "signing-key"),
		filepath.Join(f.checkpoints, "0001.json"),
	}
	for _, path := range paths {
		writeOwned(t, path, "owned")
	}
	model := filepath.Join(f.state, "manager", "ollama-models", "blobs", "sha256-model")
	writeOwned(t, model, "downloaded model")
	normalHermesProfile := filepath.Join(f.home, ".hermes", "profiles", "normal", "config.yaml")
	operatorOllamaModel := filepath.Join(f.home, ".ollama", "models", "blobs", "sha256-external")
	writeOwned(t, normalHermesProfile, "profile")
	writeOwned(t, operatorOllamaModel, "model")

	plan, err := f.service.Plan(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Artifacts) < len(paths)+1 {
		t.Fatalf("incomplete plan: %+v", plan.Artifacts)
	}
	if err = f.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if inspection := config.Inspect(f.config); inspection.State != config.StateAbsent {
		t.Fatalf("inspection=%+v", inspection)
	}
	if data, readErr := os.ReadFile(model); readErr != nil || string(data) != "downloaded model" {
		t.Fatalf("model was not preserved: %q %v", data, readErr)
	}
	for _, preserved := range []string{normalHermesProfile, operatorOllamaModel} {
		if _, statErr := os.Stat(preserved); statErr != nil {
			t.Fatalf("external runtime asset changed %s: %v", preserved, statErr)
		}
	}
	for _, path := range paths {
		if _, statErr := os.Lstat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("artifact retained %s: %v", path, statErr)
		}
	}

	initializer := initialize.New()
	initPlan, err := initializer.Plan(f.config, f.state)
	if err != nil {
		t.Fatal(err)
	}
	if err = initializer.Apply(context.Background(), initPlan); err != nil {
		t.Fatal(err)
	}
	if inspection := config.Inspect(f.config); inspection.State != config.StateValid {
		t.Fatalf("reinitialized inspection=%+v", inspection)
	}
}

func TestPlanIsDeterministicAndExact(t *testing.T) {
	f := newFixture(t)
	f.writeConfig(t, "")
	writeOwned(t, filepath.Join(f.state, "plans", "b.json"), "b")
	writeOwned(t, filepath.Join(f.state, "plans", "a.json"), "a")
	first, err := f.service.Plan(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.service.Plan(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	if PlanDigest(first) != PlanDigest(second) {
		t.Fatalf("digest changed: %s != %s", PlanDigest(first), PlanDigest(second))
	}
	if len(first.Artifacts) != len(second.Artifacts) {
		t.Fatal("artifact count changed")
	}
	for index := range first.Artifacts {
		if first.Artifacts[index].Path != second.Artifacts[index].Path {
			t.Fatalf("order changed: %+v", first.Artifacts)
		}
	}
}

func TestResetStatesAndAuthentication(t *testing.T) {
	t.Run("absent idempotent", func(t *testing.T) {
		f := newFixture(t)
		plan, err := f.service.Plan(context.Background(), f.config)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.service.Apply(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("recognized partial", func(t *testing.T) {
		f := newFixture(t)
		partial := filepath.Join(filepath.Dir(f.config), config.InitializationTemporaryPrefix+"interrupted")
		writeOwned(t, partial, "partial")
		plan, err := f.service.Plan(context.Background(), f.config)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.service.Apply(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
		if config.Inspect(f.config).State != config.StateAbsent {
			t.Fatal("partial reset did not produce absence")
		}
	})
	t.Run("malformed secure file", func(t *testing.T) {
		f := newFixture(t)
		writeOwned(t, f.config, "principal: [\n")
		plan, err := f.service.Plan(context.Background(), f.config)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.service.Apply(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
		if config.Inspect(f.config).State != config.StateAbsent {
			t.Fatal("malformed config retained")
		}
	})
	t.Run("malformed paths are not deletion authority", func(t *testing.T) {
		f := newFixture(t)
		victim := filepath.Join(f.home, "victim", "keep.txt")
		writeOwned(t, victim, "preserve")
		writeOwned(t, f.config, fmt.Sprintf("state_dir: %q\nprincipal: [\n", filepath.Dir(victim)))
		plan, err := f.service.Plan(context.Background(), f.config)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.service.Apply(context.Background(), plan); err != nil {
			t.Fatal(err)
		}
		if data, err := os.ReadFile(victim); err != nil || string(data) != "preserve" {
			t.Fatalf("malformed path was trusted: %q %v", data, err)
		}
	})
	t.Run("insecure config", func(t *testing.T) {
		f := newFixture(t)
		writeOwned(t, f.config, "principal: [\n")
		if err := os.Chmod(f.config, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Plan(context.Background(), f.config); err == nil || !strings.Contains(err.Error(), ReasonDenied) {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("wrong principal", func(t *testing.T) {
		f := newFixture(t)
		f.writeConfig(t, "")
		f.service.Current = func() (*user.User, error) { return &user.User{Uid: "99999", Username: "other"}, nil }
		f.service.LookupID = func(string) (*user.User, error) { return &user.User{Uid: "99999", Username: "other"}, nil }
		if _, err := f.service.Plan(context.Background(), f.config); err == nil || !strings.Contains(err.Error(), "does not exactly match") {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestUnsafeAndChangedArtifactsFailClosed(t *testing.T) {
	t.Run("symlink config", func(t *testing.T) {
		f := newFixture(t)
		target := filepath.Join(f.home, "target")
		writeOwned(t, target, "principal: [\n")
		if err := os.MkdirAll(filepath.Dir(f.config), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, f.config); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Plan(context.Background(), f.config); err == nil {
			t.Fatal("symlink accepted")
		}
	})
	t.Run("symlink state", func(t *testing.T) {
		f := newFixture(t)
		f.writeConfig(t, "")
		real := filepath.Join(f.home, "real")
		if err := os.Mkdir(real, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, f.state); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Plan(context.Background(), f.config); err == nil {
			t.Fatal("state symlink accepted")
		}
	})
	t.Run("unknown state file", func(t *testing.T) {
		f := newFixture(t)
		f.writeConfig(t, "")
		unknown := filepath.Join(f.state, "do-not-delete.txt")
		writeOwned(t, unknown, "operator data")
		if _, err := f.service.Plan(context.Background(), f.config); err == nil || !strings.Contains(err.Error(), "unknown artifact") {
			t.Fatalf("error=%v", err)
		}
		if _, err := os.Stat(unknown); err != nil {
			t.Fatal("unknown file changed")
		}
	})
	t.Run("hard linked artifact", func(t *testing.T) {
		f := newFixture(t)
		f.writeConfig(t, "")
		path := filepath.Join(f.state, "plans", "one.json")
		writeOwned(t, path, "owned")
		if err := os.Link(path, filepath.Join(f.home, "second-link")); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Plan(context.Background(), f.config); err == nil || !strings.Contains(err.Error(), "hard links") {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("writable parent", func(t *testing.T) {
		f := newFixture(t)
		f.writeConfig(t, "")
		if err := os.Chmod(filepath.Dir(f.config), 0770); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Plan(context.Background(), f.config); err == nil || !strings.Contains(err.Error(), "writable by group") {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("inode changed", func(t *testing.T) {
		f := newFixture(t)
		f.writeConfig(t, "")
		path := filepath.Join(f.state, "plans", "one.json")
		writeOwned(t, path, "one")
		plan, err := f.service.Plan(context.Background(), f.config)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Remove(path); err != nil {
			t.Fatal(err)
		}
		writeOwned(t, path, "replacement")
		if err = f.service.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), ReasonChanged) {
			t.Fatalf("error=%v", err)
		}
		if _, err = os.Stat(f.config); err != nil {
			t.Fatal("config removed after changed plan")
		}
	})
	t.Run("repository path", func(t *testing.T) {
		f := newFixture(t)
		repo := filepath.Join(f.home, "source")
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(repo, "aegis.yaml")
		writeOwned(t, configPath, "principal: [\n")
		if _, err := f.service.Plan(context.Background(), configPath); err == nil || !strings.Contains(err.Error(), "repository") {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("custom state outside authenticated home", func(t *testing.T) {
		f := newFixture(t)
		outside := filepath.Join(filepath.Dir(f.home), "outside-state")
		f.writeConfig(t, "")
		document, err := os.ReadFile(f.config)
		if err != nil {
			t.Fatal(err)
		}
		document = []byte(strings.Replace(string(document), fmt.Sprintf("state_dir: %q", f.state), fmt.Sprintf("state_dir: %q", outside), 1))
		if err = os.WriteFile(f.config, document, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.Plan(context.Background(), f.config); err == nil || !strings.Contains(err.Error(), ReasonDenied) {
			t.Fatalf("error=%v", err)
		}
	})
}

func TestExternalAndSystemdCustodyArePreserved(t *testing.T) {
	f := newFixture(t)
	externalDB := filepath.Join(f.home, "external-authority", "authority.db")
	writeOwned(t, externalDB, "external")
	f.writeConfig(t, fmt.Sprintf("credentials:\n  authority:\n    database: %q\n    deployment_id: deployment-one\n    custody: systemd\n    kek_credential: aegis-kek\n", externalDB))
	plan, err := f.service.Plan(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan.Preserved, "\n")
	if !strings.Contains(joined, externalDB) || !strings.Contains(joined, "systemd KEK credential") {
		t.Fatalf("preservation preview=%s", joined)
	}
	if plan.CredentialRecords || plan.LocalKEK {
		t.Fatalf("external material marked for destruction: %+v", plan)
	}
	if err = f.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(externalDB); err != nil {
		t.Fatalf("external database removed: %v", err)
	}
}

func TestOwnedDevelopmentAuthorityAndKEKAreDestroyed(t *testing.T) {
	f := newFixture(t)
	database := filepath.Join(f.state, "authority", "authority.db")
	kek := filepath.Join(f.state, "authority", "authority-kek.json")
	f.writeConfig(t, fmt.Sprintf("credentials:\n  authority:\n    database: %q\n    deployment_id: deployment-one\n    custody: host-file\n    kek_file: %q\n", database, kek))
	if err := credentials.CreateHostKey(kek, "host-kek"); err != nil {
		t.Fatal(err)
	}
	custodian, err := credentials.LoadFileCustodian(kek)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := credentialbolt.Open(context.Background(), database, "deployment-one", custodian)
	if err != nil {
		t.Fatal(err)
	}
	if err = authority.Close(); err != nil {
		t.Fatal(err)
	}
	custodian.Close()
	plan, err := f.service.Plan(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.CredentialRecords || !plan.LocalKEK {
		t.Fatalf("destruction not disclosed: %+v", plan)
	}
	if err = f.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{database, kek} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("authority artifact retained %s: %v", path, statErr)
		}
	}
}

func TestSymlinkedAuthorityAndKEKFailClosed(t *testing.T) {
	for _, target := range []string{"database", "kek"} {
		t.Run(target, func(t *testing.T) {
			f := newFixture(t)
			database := filepath.Join(f.state, "authority", "authority.db")
			kek := filepath.Join(f.state, "authority", "authority-kek.json")
			f.writeConfig(t, fmt.Sprintf("credentials:\n  authority:\n    database: %q\n    deployment_id: deployment-one\n    custody: host-file\n    kek_file: %q\n", database, kek))
			real := filepath.Join(f.home, "external", target)
			writeOwned(t, real, "external")
			path := database
			if target == "kek" {
				path = kek
			}
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, path); err != nil {
				t.Fatal(err)
			}
			if _, err := f.service.Plan(context.Background(), f.config); err == nil || !strings.Contains(err.Error(), ReasonDenied) {
				t.Fatalf("error=%v", err)
			}
			if _, err := os.Stat(real); err != nil {
				t.Fatalf("external target changed: %v", err)
			}
		})
	}
}

func TestMissingPreviewedArtifactIsIdempotentlyAbsent(t *testing.T) {
	f := newFixture(t)
	f.writeConfig(t, "")
	path := filepath.Join(f.state, "plans", "one.json")
	writeOwned(t, path, "{}")
	plan, err := f.service.Plan(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = f.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if inspection := config.Inspect(f.config); inspection.State != config.StateAbsent {
		t.Fatalf("inspection=%+v", inspection)
	}
}

func TestEnvironmentPathOverrideIsNotDeletionAuthority(t *testing.T) {
	f := newFixture(t)
	f.writeConfig(t, "")
	external := filepath.Join(f.home, "environment-selected")
	writeOwned(t, filepath.Join(external, "plans", "one.json"), "{}")
	t.Setenv("AEGIS_STATE_DIR", external)
	if _, err := f.service.Plan(context.Background(), f.config); err == nil || !strings.Contains(err.Error(), "not deletion authority") {
		t.Fatalf("error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(external, "plans", "one.json")); err != nil {
		t.Fatalf("environment-selected state changed: %v", err)
	}
}

func TestConfiguredExternalTLSFilesArePreserved(t *testing.T) {
	f := newFixture(t)
	certificate := filepath.Join(f.state, "external-tls", "cert.pem")
	privateKey := filepath.Join(f.state, "external-tls", "key.pem")
	f.writeConfig(t, fmt.Sprintf("api:\n  tls_cert_file: %q\n  tls_key_file: %q\n", certificate, privateKey))
	writeOwned(t, certificate, "certificate")
	writeOwned(t, privateKey, "private key")
	plan, err := f.service.Plan(context.Background(), f.config)
	if err != nil {
		t.Fatal(err)
	}
	preview := strings.Join(plan.Preserved, "\n")
	if !strings.Contains(preview, certificate) || !strings.Contains(preview, privateKey) {
		t.Fatalf("preservation preview=%s", preview)
	}
	if err = f.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{certificate, privateKey} {
		if _, err = os.Stat(path); err != nil {
			t.Fatalf("external TLS artifact changed %s: %v", path, err)
		}
	}
}
