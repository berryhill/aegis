package command

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildVersionSelectsFixedExecutionProfile(t *testing.T) {
	if got := ProfileForVersion("dev"); got != DevelopmentProfile {
		t.Fatalf("dev profile=%q", got)
	}
	if got := ProfileForVersion("0.1.27"); got != ProductionProfile {
		t.Fatalf("release profile=%q", got)
	}
	if got := ProfileForVersion("test"); got != "" {
		t.Fatalf("test profile=%q", got)
	}
}

func TestStableCheckoutExecutableSelectsDevelopmentProfile(t *testing.T) {
	home := t.TempDir()
	repository := filepath.Join(home, "repository")
	if err := os.MkdirAll(filepath.Join(repository, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "go.mod"), []byte("module github.com/berryhill/aegis\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(repository, "aegis")
	if err := os.WriteFile(executable, []byte("binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := ProfileForExecutable("0.1.27", executable); got != DevelopmentProfile {
		t.Fatalf("stable checkout profile=%q", got)
	}
	installed := filepath.Join(home, "bin", "aegis")
	if err := os.MkdirAll(filepath.Dir(installed), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := ProfileForExecutable("0.1.27", installed); got != ProductionProfile {
		t.Fatalf("installed stable profile=%q", got)
	}
}

func TestDevelopmentProfileUsesRepositoryAndRejectsProductionPaths(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	repository := filepath.Join(home, "repository")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(repository, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "go.mod"), []byte("module github.com/berryhill/aegis\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	var output bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader(""), Out: &output, Err: io.Discard, Version: "dev", Profile: DevelopmentProfile, DevelopmentRoot: repository, IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	developmentConfig := filepath.Join(repository, ".aegis", "aegis.yaml")
	developmentState := filepath.Join(repository, ".aegis", "state")
	if got := root.PersistentFlags().Lookup("config").DefValue; got != developmentConfig {
		t.Fatalf("development config default=%q want %q", got, developmentConfig)
	}
	if got := root.PersistentFlags().Lookup("state-dir").DefValue; got != developmentState {
		t.Fatalf("development state default=%q want %q", got, developmentState)
	}
	root.SetArgs([]string{"--config", filepath.Join(home, ".argis", "aegis.yaml"), "reset"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "development executable refuses reset outside its fixed profile") {
		t.Fatalf("production path accepted by development profile: %v", err)
	}
	if err := validateConfiguredPathsProfile(DevelopmentProfile, map[string]string{"state": filepath.Join(home, ".argis", "state")}); err == nil {
		t.Fatal("former production state accepted through development configuration")
	}
	if err := validateConfiguredPathsProfile(DevelopmentProfile, map[string]string{"state": filepath.Join(home, ".aegis", "state")}); err == nil {
		t.Fatal("canonical production state accepted through development configuration")
	}
	if err := validateConfiguredPathsProfile(DevelopmentProfile, map[string]string{"state": filepath.Join(repository, ".aegis-fixture", "state")}); err != nil {
		t.Fatalf("isolated repository fixture rejected: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".argis", "state"), 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(repository, "production-state-alias")
	if err := os.Symlink(filepath.Join(home, ".argis", "state"), alias); err != nil {
		t.Fatal(err)
	}
	if err := validateConfiguredPathsProfile(DevelopmentProfile, map[string]string{"credential database": alias}); err == nil {
		t.Fatal("symlink alias to production state accepted")
	}
}

func TestProductionProfileUsesCanonicalRootAndRefusesFormerRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	resolved, err := resolveExecutionProfile(ProductionProfile, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".aegis"); resolved.Root != want {
		t.Fatalf("production root=%q want %q", resolved.Root, want)
	}
	formerRoot := filepath.Join(home, ".argis")
	if err := os.Mkdir(formerRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(formerRoot, "aegis.yaml"), []byte("malformed: [\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	root := NewRoot(Dependencies{
		In:         strings.NewReader(""),
		Out:        &output,
		Err:        io.Discard,
		Version:    "0.1.27",
		Profile:    ProductionProfile,
		IsTerminal: func(io.Reader, io.Writer) bool { return true },
	})
	root.SetArgs(nil)
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "migrate-layout") {
		t.Fatalf("former production root did not require explicit migration: %v", err)
	}
	if got := output.String(); got != "" {
		t.Fatalf("lifecycle denial emitted startup output before admission: %q", got)
	}
	if _, statErr := os.Lstat(filepath.Join(home, ".aegis")); !os.IsNotExist(statErr) {
		t.Fatalf("denied startup initialized canonical root beside former root: %v", statErr)
	}
}

func TestDevelopmentProfileDerivesEveryOperationalPathFromRepositoryCustody(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repository := filepath.Join(home, "repository")
	if err := os.Mkdir(repository, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repository, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "go.mod"), []byte("module github.com/berryhill/aegis\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveExecutionProfile(DevelopmentProfile, repository)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(repository, ".aegis")
	for name, path := range map[string]string{
		"config": got.Config, "state": got.State, "audit": got.AuditCheckpoints,
		"authority": got.AuthorityPersistence, "credentials": got.CredentialDatabase,
		"kek": got.HostKEK, "certifications": got.ManagerCertifications,
		"models": got.ManagedModels, "runtime": got.Runtime,
	} {
		if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
			t.Errorf("%s escaped development custody: %q", name, path)
		}
	}
}

func TestDevelopmentProfileRequiresAegisRepositoryRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := resolveExecutionProfile(DevelopmentProfile, root); err == nil {
		t.Fatal("non-repository development root accepted")
	}
}

func TestDevelopmentProfileRejectsPreRenameState(t *testing.T) {
	home := t.TempDir()
	repository := filepath.Join(home, "repository")
	if err := os.MkdirAll(filepath.Join(repository, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, "go.mod"), []byte("module github.com/berryhill/aegis\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repository, ".aegis-dev"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	if _, err := resolveExecutionProfile(DevelopmentProfile, repository); err == nil || !strings.Contains(err.Error(), "migrate it explicitly") {
		t.Fatalf("pre-rename development state accepted: %v", err)
	}
}
