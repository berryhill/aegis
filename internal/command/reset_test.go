package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	resetdomain "github.com/berryhill/aegis/internal/reset"
	"github.com/spf13/cobra"
)

type resetCommandFixture struct {
	home, config, state string
	service             *resetdomain.Service
}

func newResetCommandFixture(t *testing.T, initialized bool) resetCommandFixture {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "home")
	if err = os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(home, ".config", "aegis", "aegis.yaml")
	state := filepath.Join(home, "state")
	copyUser := *current
	service := &resetdomain.Service{
		Current: func() (*user.User, error) { value := copyUser; return &value, nil },
		LookupID: func(uid string) (*user.User, error) {
			value := copyUser
			if uid != value.Uid {
				return nil, errors.New("unknown uid")
			}
			return &value, nil
		},
		HomeDir: func() (string, error) { return home, nil },
	}
	if initialized {
		if err = os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
			t.Fatal(err)
		}
		document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\n", state, current.Uid, current.Username, filepath.Join(state, "audit-checkpoints"))
		if err = os.WriteFile(configPath, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.MkdirAll(filepath.Join(state, "plans"), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(state, "plans", "one.json"), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return resetCommandFixture{home: home, config: configPath, state: state, service: service}
}

func executeReset(t *testing.T, fixture resetCommandFixture, input string, terminal bool, ctx context.Context) (string, error) {
	t.Helper()
	passphrase := addResetPassphraseAuthority(t, fixture)
	provider := &sequencePassphrases{values: [][]byte{append([]byte(nil), passphrase...), append([]byte(nil), passphrase...)}}
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader(input), Out: &out, Err: io.Discard, Version: "test", Passphrases: provider, IsTerminal: func(io.Reader, io.Writer) bool { return terminal }, Resetter: fixture.service})
	root.SetArgs([]string{"--config", fixture.config, "reset"})
	if ctx != nil {
		root.SetContext(ctx)
	}
	err := root.Execute()
	return out.String(), err
}

func TestResetCommandPreviewAndYesConfirmation(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	memArtifact := filepath.Join(fixture.state, "persistence", "authority-v1", "stores", "store-0123456789abcdef0123456789abcdef.badger", "00001.mem")
	fleetArtifact := filepath.Join(fixture.state, "persistence", "fleet-v1", "store.badger", "00001.mem")
	for _, artifact := range []string{memArtifact, fleetArtifact} {
		if err := os.MkdirAll(filepath.Dir(artifact), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(artifact, []byte("badger value log metadata"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := executeReset(t, fixture, "y\n", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"operation": "reset"`,
		`"resolved_config_path": "` + fixture.config + `"`,
		`"path": "` + filepath.Join(fixture.state, "plans", "one.json") + `"`,
		`"path": "` + memArtifact + `"`,
		`"path": "` + fleetArtifact + `"`,
		`"confirmation_required": "authority passphrase, then y/yes, then authority passphrase again"`,
		`"gateway_action": "stop and purge exact Aegis-owned user gateway if installed"`,
		"Apply this exact reset plan? [y/N]",
		`"credential_records_destroyed": true`,
		`"local_kek_destroyed": true`,
		"encrypted credentials and audit history",
		"Hermes installation and normal Hermes profiles",
		"Ollama installation, operator-managed daemon, and downloaded model stores",
		`"state": "uninitialized"`,
		`"next_command": "aegis"`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("preview missing %q:\n%s", expected, output)
		}
	}
	if config.Inspect(fixture.config).State != config.StateAbsent {
		t.Fatal("reset did not produce absent config")
	}
	if _, err = os.Lstat(memArtifact); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Badger memory-table artifact remains after reset: %v", err)
	}
	if _, err = os.Lstat(fleetArtifact); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fleet Badger memory-table artifact remains after reset: %v", err)
	}
}

func TestProductionBareResetSelectsDiscoveredFormerLayout(t *testing.T) {
	fixture := newResetCommandFixture(t, false)
	t.Setenv("HOME", fixture.home)
	fixture.config = filepath.Join(fixture.home, ".argis", "aegis.yaml")
	fixture.state = filepath.Join(fixture.home, ".argis", "state")
	if err := os.MkdirAll(filepath.Dir(fixture.config), 0700); err != nil {
		t.Fatal(err)
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\n", fixture.state, current.Uid, current.Username, filepath.Join(fixture.state, "audit-checkpoints"))
	if err = os.WriteFile(fixture.config, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	legacyArtifact := filepath.Join(fixture.state, "plans", "legacy.json")
	if err = os.MkdirAll(filepath.Dir(legacyArtifact), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(legacyArtifact, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	passphrase := addResetPassphraseAuthority(t, fixture)
	provider := &sequencePassphrases{values: [][]byte{append([]byte(nil), passphrase...), append([]byte(nil), passphrase...)}}
	var output bytes.Buffer
	root := NewRoot(Dependencies{
		In:          strings.NewReader(resetdomain.Confirmation + "\n"),
		Out:         &output,
		Err:         io.Discard,
		Version:     "0.1.27",
		Passphrases: provider,
		Profile:     ProductionProfile,
		IsTerminal:  func(io.Reader, io.Writer) bool { return true },
		Resetter:    fixture.service,
	})
	root.SetArgs([]string{"reset"})
	if err = root.Execute(); err != nil {
		t.Fatal(err)
	}
	preview := output.String()
	for _, expected := range []string{fixture.config, legacyArtifact, `"configuration_state": "valid"`, `"state": "uninitialized"`} {
		if !strings.Contains(preview, expected) {
			t.Fatalf("bare production reset preview missing %q:\n%s", expected, preview)
		}
	}
	entries, err := os.ReadDir(fixture.state)
	if err != nil || len(entries) != 0 {
		t.Fatalf("retained former state directory not empty: entries=%v err=%v", entries, err)
	}
	if inspection := config.Inspect(""); inspection.State != config.StateAbsent {
		t.Fatalf("default discovery after reset=%+v", inspection)
	}
}

func TestResetCommandDeclineEOFNonTTYAndCancellationWriteNothing(t *testing.T) {
	for _, test := range []struct {
		name, input string
		terminal    bool
		cancel      bool
		wantError   bool
		wantReason  string
	}{
		{name: "default no", input: "\n", terminal: true},
		{name: "explicit no", input: "no\n", terminal: true},
		{name: "old phrase", input: "reset aegis\n", terminal: true},
		{name: "eof", input: "", terminal: true},
		{name: "non tty", input: resetdomain.Confirmation + "\n", terminal: false, wantError: true, wantReason: resetdomain.ReasonRequiresTTY},
		{name: "cancellation", input: resetdomain.Confirmation + "\n", terminal: true, cancel: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newResetCommandFixture(t, true)
			ctx := context.Background()
			if test.cancel {
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			output, err := executeReset(t, fixture, test.input, test.terminal, ctx)
			if test.wantError != (err != nil) {
				t.Fatalf("error=%v output=%s", err, output)
			}
			if test.wantReason != "" && !strings.Contains(err.Error(), test.wantReason) {
				t.Fatalf("error=%v does not contain %s", err, test.wantReason)
			}
			if _, statErr := os.Stat(fixture.config); statErr != nil {
				t.Fatalf("config changed: %v", statErr)
			}
			if _, statErr := os.Stat(filepath.Join(fixture.state, "plans", "one.json")); statErr != nil {
				t.Fatalf("state changed: %v", statErr)
			}
		})
	}
}

func TestResetCommandDetectsChangeBetweenPreviewAndApply(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	fixture.service.BeforeApply = func(resetdomain.Plan) {
		path := filepath.Join(fixture.state, "plans", "one.json")
		_ = os.Remove(path)
		_ = os.WriteFile(path, []byte("changed"), 0600)
	}
	_, err := executeReset(t, fixture, resetdomain.Confirmation+"\n", true, nil)
	if err == nil || !strings.Contains(err.Error(), resetdomain.ReasonChanged) {
		t.Fatalf("error=%v", err)
	}
	if _, statErr := os.Stat(fixture.config); statErr != nil {
		t.Fatalf("config changed: %v", statErr)
	}
}

func TestResetCredentialAuthorityRequiresExistingPassphrase(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	passphrase := make([]byte, 24)
	if _, err := rand.Read(passphrase); err != nil {
		t.Fatal(err)
	}
	kekPath := filepath.Join(fixture.state, "credentials", "authority.kek")
	if err := credentials.CreatePassphraseKey(kekPath, "reset-test-kek", passphrase); err != nil {
		t.Fatal(err)
	}
	document, err := os.ReadFile(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(fixture.state, "credentials", "authority.db")
	document = append(document, []byte(fmt.Sprintf("credentials:\n  authority:\n    database: %q\n    deployment_id: reset-test\n    custody: passphrase-file\n    kek_file: %q\n", database, kekPath))...)
	if err = os.WriteFile(fixture.config, document, 0600); err != nil {
		t.Fatal(err)
	}
	plan := resetdomain.Plan{ConfigPath: fixture.config, CredentialRecords: true, LocalKEK: true}
	command := &cobra.Command{}
	provider := &sequencePassphrases{values: [][]byte{append([]byte(nil), passphrase...)}}
	command.SetContext(context.WithValue(context.Background(), authorityPassphraseContextKey{}, AuthorityPassphraseProvider(provider)))
	command.SetIn(strings.NewReader(""))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	if err = authenticateResetAuthority(command, plan); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 {
		t.Fatalf("passphrase requests=%d want 1", provider.calls)
	}

	wrong := make([]byte, len(passphrase))
	if _, err = rand.Read(wrong); err != nil {
		t.Fatal(err)
	}
	provider = &sequencePassphrases{values: [][]byte{wrong, wrong, wrong}}
	command.SetContext(context.WithValue(context.Background(), authorityPassphraseContextKey{}, AuthorityPassphraseProvider(provider)))
	if err = authenticateResetAuthority(command, plan); err == nil || !strings.Contains(err.Error(), resetdomain.ReasonRequiresAuthority) {
		t.Fatalf("wrong passphrase accepted: %v", err)
	}
	if _, err = os.Stat(kekPath); err != nil {
		t.Fatalf("failed authentication mutated authority: %v", err)
	}
}

func TestResetAuthenticationPolicyDiffersByExecutionProfile(t *testing.T) {
	for _, test := range []struct {
		name         string
		profile      ExecutionProfile
		wantCalls    int
		wantRequired bool
		confirmation string
	}{
		{name: "production authenticates twice", profile: ProductionProfile, wantCalls: 2, wantRequired: true, confirmation: "authority passphrase, then y/yes, then authority passphrase again"},
		{name: "development skips authentication", profile: DevelopmentProfile, wantCalls: 0, wantRequired: false, confirmation: "y/yes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newResetCommandFixture(t, true)
			_ = addResetPassphraseAuthority(t, fixture)
			calls := 0
			authenticate := func(*cobra.Command, resetdomain.Plan) error {
				calls++
				return nil
			}
			var output bytes.Buffer
			command := resetCmdWithAuthenticator(fixture.service, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: fixture.config}, test.profile, authenticate)
			command.SetIn(strings.NewReader("yes\n"))
			command.SetOut(&output)
			command.SetErr(io.Discard)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if calls != test.wantCalls {
				t.Fatalf("authority authentications=%d want %d", calls, test.wantCalls)
			}
			preview := output.String()
			if !strings.Contains(preview, fmt.Sprintf(`"authority_passphrase_required": %t`, test.wantRequired)) || !strings.Contains(preview, fmt.Sprintf(`"authority_passphrase_authentications": %d`, test.wantCalls)) || !strings.Contains(preview, fmt.Sprintf(`"confirmation_required": %q`, test.confirmation)) {
				t.Fatalf("incorrect authentication preview: %s", preview)
			}
		})
	}
}

func TestProductionResetRequiresAuthorityBeforeGatewayOrStateMutation(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	statePath := filepath.Join(fixture.state, "plans", "one.json")
	beforeConfig, err := os.ReadFile(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	purges := 0
	command := resetCmdWithHooks(
		fixture.service,
		func(io.Reader, io.Writer) bool { return true },
		&rootOptions{configFile: fixture.config},
		ProductionProfile,
		authenticateResetAuthority,
		func(context.Context, string) (bool, error) {
			purges++
			return true, nil
		},
	)
	command.SetIn(strings.NewReader("yes\n"))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	if err = command.Execute(); err == nil || !strings.Contains(err.Error(), resetdomain.ReasonRequiresAuthority) {
		t.Fatalf("production reset without authority error=%v", err)
	}
	if purges != 0 {
		t.Fatalf("unauthenticated production reset reached gateway purge %d time(s)", purges)
	}
	afterConfig, err := os.ReadFile(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	afterState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeConfig, afterConfig) || !bytes.Equal(beforeState, afterState) {
		t.Fatal("unauthenticated production reset changed gateway or state inputs")
	}
}

func TestProductionResetSecondAuthenticationFailureWritesNothing(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	_ = addResetPassphraseAuthority(t, fixture)
	calls := 0
	authenticate := func(*cobra.Command, resetdomain.Plan) error {
		calls++
		if calls == 2 {
			return errors.New("second authentication rejected")
		}
		return nil
	}
	command := resetCmdWithAuthenticator(fixture.service, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: fixture.config}, ProductionProfile, authenticate)
	command.SetIn(strings.NewReader("yes\n"))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "second authentication rejected") {
		t.Fatalf("second authentication failure not returned: %v", err)
	}
	if calls != 2 {
		t.Fatalf("authority authentications=%d want 2", calls)
	}
	for _, path := range []string{fixture.config, filepath.Join(fixture.state, "credentials", "authority.db"), filepath.Join(fixture.state, "credentials", "authority.kek")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("second authentication failure changed %s: %v", path, err)
		}
	}
}

func TestProductionResetPurgesGatewayBeforeDeletingState(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	_ = addResetPassphraseAuthority(t, fixture)
	calls := 0
	purges := 0
	authenticate := func(*cobra.Command, resetdomain.Plan) error {
		calls++
		return nil
	}
	purge := func(ctx context.Context, configPath string) (bool, error) {
		purges++
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if configPath != fixture.config {
			t.Fatalf("gateway purge config=%q want %q", configPath, fixture.config)
		}
		if _, err := os.Stat(fixture.config); err != nil {
			t.Fatalf("gateway purge ran after configuration deletion: %v", err)
		}
		if err := os.Remove(filepath.Join(fixture.state, "plans", "one.json")); err != nil {
			t.Fatalf("simulate gateway volatile-artifact cleanup: %v", err)
		}
		return true, nil
	}
	command := resetCmdWithHooks(fixture.service, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: fixture.config}, ProductionProfile, authenticate, purge)
	command.SetIn(strings.NewReader("yes\n"))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || purges != 1 {
		t.Fatalf("authentications=%d purges=%d want 2,1", calls, purges)
	}
	if _, err := os.Stat(fixture.config); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("configuration survived reset: %v", err)
	}
}

func TestProductionResetGatewayPurgeFailurePreservesState(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	purgeErr := errors.New("gateway stop failed")
	command := resetCmdWithHooks(
		fixture.service,
		func(io.Reader, io.Writer) bool { return true },
		&rootOptions{configFile: fixture.config},
		ProductionProfile,
		func(*cobra.Command, resetdomain.Plan) error { return nil },
		func(context.Context, string) (bool, error) { return false, purgeErr },
	)
	command.SetIn(strings.NewReader("yes\n"))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	if err := command.Execute(); !errors.Is(err, purgeErr) {
		t.Fatalf("gateway purge failure=%v want %v", err, purgeErr)
	}
	for _, path := range []string{fixture.config, filepath.Join(fixture.state, "plans", "one.json")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("gateway purge failure changed %s: %v", path, err)
		}
	}
}

func TestProductionResetAuthenticatesRealAuthorityTwice(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	passphrase := addResetPassphraseAuthority(t, fixture)
	provider := &sequencePassphrases{values: [][]byte{append([]byte(nil), passphrase...), append([]byte(nil), passphrase...)}}
	command := resetCmd(fixture.service, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: fixture.config}, ProductionProfile)
	command.SetContext(context.WithValue(context.Background(), authorityPassphraseContextKey{}, AuthorityPassphraseProvider(provider)))
	command.SetIn(strings.NewReader("yes\n"))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("authority passphrase requests=%d want 2", provider.calls)
	}
}

func addResetPassphraseAuthority(t *testing.T, fixture resetCommandFixture) []byte {
	t.Helper()
	passphrase := make([]byte, 24)
	if _, err := rand.Read(passphrase); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(fixture.state, "credentials", "authority.db")
	kekPath := filepath.Join(fixture.state, "credentials", "authority.kek")
	if err := credentials.CreatePassphraseKey(kekPath, "reset-policy-kek", passphrase); err != nil {
		t.Fatal(err)
	}
	custodian, err := credentials.LoadPassphraseCustodian(kekPath, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := credentialbolt.Open(context.Background(), database, "reset-policy", custodian)
	if err != nil {
		custodian.Close()
		t.Fatal(err)
	}
	if err = authority.Close(); err != nil {
		custodian.Close()
		t.Fatal(err)
	}
	custodian.Close()
	document, err := os.ReadFile(fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	document = append(document, []byte(fmt.Sprintf("credentials:\n  authority:\n    database: %q\n    deployment_id: reset-policy\n    custody: passphrase-file\n    kek_file: %q\n", database, kekPath))...)
	if err = os.WriteFile(fixture.config, document, 0600); err != nil {
		t.Fatal(err)
	}
	return passphrase
}

func TestResetPreparationReleasesGatewayAuthorityBeforeStrictPlan(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	passphrase := addResetPassphraseAuthority(t, fixture)
	kekPath := filepath.Join(fixture.state, "credentials", "authority.kek")
	database := filepath.Join(fixture.state, "credentials", "authority.db")
	custodian, err := credentials.LoadPassphraseCustodian(kekPath, passphrase)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := credentialbolt.Open(context.Background(), database, "reset-policy", custodian)
	if err != nil {
		custodian.Close()
		t.Fatal(err)
	}
	closed := false
	prepare := func(*cobra.Command, string) error {
		closed = true
		if closeErr := authority.Close(); closeErr != nil {
			return closeErr
		}
		custodian.Close()
		return nil
	}
	t.Cleanup(func() {
		if !closed {
			_ = authority.Close()
			custodian.Close()
		}
	})
	purge := func(context.Context, string) (bool, error) { return false, nil }
	command := resetCmdWithPreparation(fixture.service, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: fixture.config}, DevelopmentProfile, func(*cobra.Command, resetdomain.Plan) error { return nil }, prepare, purge)
	command.SetIn(strings.NewReader("yes\n"))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	if err = command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("strict reset plan ran before gateway authority ownership was released")
	}
	if _, err = os.Stat(fixture.config); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reset did not remove configuration after prepared strict plan: %v", err)
	}
}

func TestDevelopmentResetSecuresStoppedDirtyOperationalAuthorityBeforeStrictPlan(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	authorityRoot := filepath.Join(fixture.state, "persistence", "authority-v1")
	generation, err := authoritybadger.Initialize(context.Background(), authorityRoot)
	if err != nil {
		t.Fatal(err)
	}
	store, err := authoritybadger.Open(context.Background(), authorityRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(authorityRoot, "CLEAN")); err != nil {
		t.Fatal(err)
	}
	active, err := os.ReadFile(filepath.Join(authorityRoot, "ACTIVE"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(authorityRoot, "DIRTY"), active, 0600); err != nil {
		t.Fatal(err)
	}
	vlog := filepath.Join(authorityRoot, "stores", generation.Directory, "000001.vlog")
	if err = os.Chmod(vlog, 0664); err != nil {
		t.Fatal(err)
	}
	purge := func(context.Context, string) (bool, error) { return false, nil }
	command := resetCmdWithPreparation(fixture.service, func(io.Reader, io.Writer) bool { return true }, &rootOptions{configFile: fixture.config}, DevelopmentProfile, func(*cobra.Command, resetdomain.Plan) error { return nil }, nil, purge)
	command.SetIn(strings.NewReader("yes\n"))
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	if err = command.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(fixture.config); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reset did not remove configuration after securing dirty authority: %v", err)
	}
}

func TestResetThenBareOnboardingAndNonTTYBoundary(t *testing.T) {
	fixture := newResetCommandFixture(t, true)
	if _, err := executeReset(t, fixture, resetdomain.Confirmation+"\n", true, nil); err != nil {
		t.Fatal(err)
	}

	var interactive bytes.Buffer
	provider := &sequencePassphrases{values: [][]byte{[]byte("principal-password")}}
	root := NewRoot(Dependencies{In: strings.NewReader("yes\n/status\n/quit\n"), Out: &interactive, Err: io.Discard, Version: "test", Passphrases: provider, IsTerminal: func(io.Reader, io.Writer) bool { return true }})
	root.SetArgs([]string{"--config", fixture.config, "--state-dir", fixture.state})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(interactive.String(), "Aegis first-run initialization") {
		t.Fatalf("onboarding not replayed: %s", interactive.String())
	}
	if config.Inspect(fixture.config).State != config.StateValid {
		t.Fatal("onboarding did not recreate valid configuration")
	}

	plan, err := fixture.service.Plan(context.Background(), fixture.config)
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	nonTTY := NewRoot(Dependencies{In: strings.NewReader("ignored"), Out: &output, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	nonTTY.SetArgs([]string{"--config", fixture.config})
	err = nonTTY.Execute()
	if err == nil || !strings.Contains(err.Error(), "manager_not_initialized") || !strings.Contains(output.String(), `"state": "uninitialized"`) {
		t.Fatalf("error=%v output=%s", err, output.String())
	}
}
