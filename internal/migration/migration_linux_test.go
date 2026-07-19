//go:build linux

package migration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	"github.com/berryhill/aegis/internal/layout"
)

type fixture struct {
	home, state, checkpoints, config string
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
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "ignored-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "ignored-state"))
	copyUser := *current
	service := &Service{Home: func() (string, error) { return home, nil }, Current: func() (*user.User, error) { v := copyUser; return &v, nil }, LookupID: func(uid string) (*user.User, error) {
		if uid != copyUser.Uid {
			return nil, errors.New("unknown")
		}
		v := copyUser
		return &v, nil
	}}
	return fixture{home: home, state: filepath.Join(home, ".local", "state", "aegis"), checkpoints: filepath.Join(home, ".local", "state", "aegis-checkpoints"), config: filepath.Join(home, ".config", "aegis", "aegis.yaml"), service: service, current: current}
}
func (f fixture) populate(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.config), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(f.state, "plans"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(f.state), 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.state, "plans", "one.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.checkpoints, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.checkpoints, "0001.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\n", f.state, f.current.Uid, f.current.Username, f.checkpoints)
	if err := os.WriteFile(f.config, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPlanAndApplyMigrateLegacyToLiteralArgis(t *testing.T) {
	f := newFixture(t)
	f.populate(t)
	external := filepath.Join(f.home, ".hermes", "profiles", "normal", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(external), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(external, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	plan, err := f.service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if plan.DestinationRoot != filepath.Join(f.home, ".argis") || plan.Confirmation != Confirmation {
		t.Fatalf("plan=%+v", plan)
	}
	if err = f.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if inspection := config.Inspect(""); inspection.State != config.StateValid || inspection.Path != filepath.Join(f.home, ".argis", "aegis.yaml") {
		t.Fatalf("inspection=%+v", inspection)
	}
	if _, err = os.Stat(filepath.Join(f.home, ".argis", "state", "plans", "one.json")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(external); err != nil || string(data) != "preserve" {
		t.Fatalf("external changed: %q %v", data, err)
	}
	for _, root := range []string{f.state, f.checkpoints} {
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("legacy root not retained empty %s: %v %v", root, entries, err)
		}
	}
}
func TestPlanRejectsUnknownSymlinkHardlinkAndCanonicalCollision(t *testing.T) {
	for _, kind := range []string{"unknown", "symlink", "hardlink", "canonical"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			f.populate(t)
			switch kind {
			case "unknown":
				os.WriteFile(filepath.Join(f.state, "foreign.txt"), []byte("x"), 0600)
			case "symlink":
				os.Symlink(filepath.Join(f.home, "target"), filepath.Join(f.state, "plans", "link.json"))
			case "hardlink":
				os.Link(filepath.Join(f.state, "plans", "one.json"), filepath.Join(f.home, "other"))
			case "canonical":
				os.Mkdir(filepath.Join(f.home, ".argis"), 0700)
			}
			if _, err := f.service.Plan(context.Background()); err == nil {
				t.Fatal("unsafe migration accepted")
			}
		})
	}
}

func TestPlanRejectsMalformedAndInsecureLegacyConfigButAccepts0775ExternalParent(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, fixture)
		accept bool
	}{
		{name: "malformed", mutate: func(t *testing.T, f fixture) { t.Helper(); os.WriteFile(f.config, []byte("malformed: [\n"), 0600) }},
		{name: "insecure", mutate: func(t *testing.T, f fixture) { t.Helper(); os.Chmod(f.config, 0644) }},
		{name: "0775-external-parent", accept: true, mutate: func(t *testing.T, f fixture) { t.Helper(); os.Chmod(filepath.Dir(f.state), 0775) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			f.populate(t)
			test.mutate(t, f)
			_, err := f.service.Plan(context.Background())
			if test.accept && err != nil {
				t.Fatal(err)
			}
			if !test.accept && err == nil {
				t.Fatal("unsafe legacy configuration accepted")
			}
		})
	}
}

func TestCancellationAndDigestDriftDoNotMutateSource(t *testing.T) {
	f := newFixture(t)
	f.populate(t)
	plan, err := f.service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = f.service.Apply(ctx, plan); err == nil {
		t.Fatal("cancelled apply succeeded")
	}
	if _, err = os.Stat(f.config); err != nil {
		t.Fatal("cancelled migration changed source")
	}
	if err = os.WriteFile(filepath.Join(f.state, "plans", "one.json"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = f.service.Apply(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "plan changed") {
		t.Fatalf("drift error=%v", err)
	}
	if _, err = os.Stat(filepath.Join(f.home, ".argis")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("drift created canonical root: %v", err)
	}
}

func TestCopyTreeAcrossFilesystems(t *testing.T) {
	shared := "/dev/shm"
	if info, err := os.Stat(shared); err != nil || !info.IsDir() {
		t.Skip("no shared-memory filesystem available")
	}
	source, err := os.MkdirTemp(shared, "aegis-migration-source-")
	if err != nil {
		t.Skipf("cannot create cross-filesystem source: %v", err)
	}
	defer os.RemoveAll(source)
	destination := filepath.Join(t.TempDir(), "destination")
	if err = os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(source, "artifact"), []byte("cross-filesystem"), 0600); err != nil {
		t.Fatal(err)
	}
	sourceInfo, _ := os.Stat(source)
	destinationInfo, _ := os.Stat(destination)
	if sourceInfo.Sys().(*syscall.Stat_t).Dev == destinationInfo.Sys().(*syscall.Stat_t).Dev {
		t.Skip("test filesystems share a device")
	}
	sourceStat := sourceInfo.Sys().(*syscall.Stat_t)
	identity := Identity{Device: uint64(sourceStat.Dev), Inode: sourceStat.Ino}
	if err = copyTree(source, destination, identity); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "artifact"))
	if err != nil || string(content) != "cross-filesystem" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestRewriteConfigChangesOnlyOwnedPathFields(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Dir(f.config), 0700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(f.home, "external-model-store")
	document := fmt.Sprintf("state_dir: %q\naudit:\n  checkpoint_dir: %q\napi:\n  token: %q\nmanager:\n  inference:\n    certification: %q\ncredentials:\n  authority:\n    database: %q\n    kek_file: %q\n", f.state, f.checkpoints, f.state, filepath.Join(f.state, "manager", "certifications", "one.json"), filepath.Join(f.state, "credentials", "authority.db"), external)
	if err := os.WriteFile(f.config, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	resolved, err := (layout.Resolver{Home: f.service.Home, EUID: os.Geteuid}).Resolve()
	if err != nil {
		t.Fatal(err)
	}
	rewritten, err := rewriteConfig(resolved)
	if err != nil {
		t.Fatal(err)
	}
	text := string(rewritten)
	for _, expected := range []string{filepath.Join(f.home, ".argis", "state"), filepath.Join(f.home, ".argis", "state", "audit-checkpoints"), external, "token: \"" + f.state + "\""} {
		if !strings.Contains(text, expected) {
			t.Fatalf("rewritten config missing preserved/rewritten value %q:\n%s", expected, text)
		}
	}
}

func TestMigrationPreservesCredentialAuthorityLinkageAndBytes(t *testing.T) {
	f := newFixture(t)
	f.populate(t)
	database := filepath.Join(f.state, "credentials", "authority.db")
	kek := filepath.Join(f.state, "credentials", "authority.kek")
	if err := credentials.CreateHostKey(kek, "migration-kek"); err != nil {
		t.Fatal(err)
	}
	custodian, err := credentials.LoadFileCustodian(kek)
	if err != nil {
		t.Fatal(err)
	}
	store, err := credentialbolt.Open(context.Background(), database, "migration-deployment", custodian)
	if err != nil {
		custodian.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		custodian.Close()
		t.Fatal(err)
	}
	custodian.Close()
	document := fmt.Sprintf("state_dir: %q\nprincipal:\n  id: principal\n  name: Principal\n  uid: %q\n  user: %q\n  auth_ttl: 5m\naudit:\n  checkpoint_dir: %q\ncredentials:\n  authority:\n    database: %q\n    deployment_id: migration-deployment\n    custody: host-file\n    kek_file: %q\n", f.state, f.current.Uid, f.current.Username, f.checkpoints, database, kek)
	if err = os.WriteFile(f.config, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	databaseBytes, _ := os.ReadFile(database)
	kekBytes, _ := os.ReadFile(kek)
	plan, err := f.service.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Apply(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	destinationDatabase := filepath.Join(f.home, ".argis", "state", "credentials", "authority.db")
	destinationKEK := filepath.Join(f.home, ".argis", "state", "credentials", "authority.kek")
	gotDatabase, _ := os.ReadFile(destinationDatabase)
	gotKEK, _ := os.ReadFile(destinationKEK)
	if string(gotDatabase) != string(databaseBytes) || string(gotKEK) != string(kekBytes) {
		t.Fatal("credential authority bytes changed during migration")
	}
	migratedCustodian, err := credentials.LoadFileCustodian(destinationKEK)
	if err != nil {
		t.Fatal(err)
	}
	defer migratedCustodian.Close()
	if err = credentialbolt.Inspect(context.Background(), destinationDatabase, "migration-deployment", migratedCustodian); err != nil {
		t.Fatal(err)
	}
}
