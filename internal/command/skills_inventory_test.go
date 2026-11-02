package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/skillbundle"
	selfupdate "github.com/berryhill/aegis/internal/update"
)

type skillsNoService struct{ t *testing.T }

func (s skillsNoService) Output(context.Context, ...string) ([]byte, error) {
	s.t.Fatal("filesystem command observed user service")
	return nil, nil
}

func (s skillsNoService) Run(context.Context, ...string) error {
	s.t.Fatal("filesystem command invoked user service")
	return nil
}

type skillsNoUpdate struct{ t *testing.T }

func (s skillsNoUpdate) Run(context.Context, bool) (selfupdate.Result, error) {
	s.t.Fatal("filesystem command invoked executable updater")
	return selfupdate.Result{}, nil
}

func runSkills(t *testing.T, ctx context.Context, args ...string) (skillbundle.InstalledInventory, error) {
	t.Helper()
	var out bytes.Buffer
	root := NewRoot(Dependencies{In: strings.NewReader(""), Out: &out, Err: io.Discard, Version: "test",
		// A broken profile must not obstruct the filesystem utility either.
		Profile: "unresolved", UserService: skillsNoService{t}, Updater: skillsNoUpdate{t}})
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	var got skillbundle.InstalledInventory
	if err == nil {
		if e := json.Unmarshal(out.Bytes(), &got); e != nil {
			t.Fatalf("expected inventory JSON, got %q: %v", out.String(), e)
		}
	}
	return got, err
}

func TestSkillsPackagedArguments(t *testing.T) {
	root := NewRoot(Dependencies{Out: io.Discard, Err: io.Discard, Version: "test"})
	group, _, err := root.Find([]string{"skills"})
	if err != nil || group.Name() != "skills" {
		t.Fatalf("missing skills group: %v", err)
	}
	var names []string
	for _, child := range group.Commands() {
		names = append(names, child.Name())
		if len(child.Aliases) != 0 || child.HasSubCommands() {
			t.Fatalf("unexpected surface: %s", child.Name())
		}
	}
	if !reflect.DeepEqual(names, []string{"install", "inventory", "rollback", "update"}) {
		t.Fatalf("unexpected children: %v", names)
	}
	for _, verb := range names {
		n := 4
		if verb == "inventory" {
			n = 1
		}
		child, _, _ := root.Find([]string{"skills", verb})
		for count := 0; count <= 5; count++ {
			if err := child.Args(child, make([]string, count)); (err == nil) != (count == n) {
				t.Errorf("%s arity %d: %v", verb, count, err)
			}
		}
		args := []string{"skills", verb}
		for i := 0; i < n; i++ {
			args = append(args, "unused")
		}
		for _, flag := range []string{"target", "config", "state-dir", "runtime", "hermes-executable", "pinentry-executable", "update", "version", "unsupported"} {
			values := []string{"", "selected"}
			if flag == "update" || flag == "version" {
				values = []string{"", "true", "false"}
			}
			for _, value := range values {
				for _, before := range []bool{true, false} {
					input := append([]string{}, args...)
					f := "--" + flag + "=" + value
					if before {
						input = append([]string{f}, input...)
					} else {
						input = append(input, f)
					}
					_, err := runSkills(t, context.Background(), input...)
					if err == nil || !strings.Contains(err.Error(), flag) {
						t.Errorf("flag should deny before filesystem access %v: %v", input, err)
					}
				}
			}
		}
	}
	for _, verb := range []string{"validate", "evaluate", "build", "verify"} {
		if _, err := runSkills(t, context.Background(), "skills", verb); err == nil {
			t.Errorf("developer command exposed: %s", verb)
		}
	}
}

func TestSkillsPackagedInventoryJourney(t *testing.T) {
	proof := t.TempDir()
	t.Setenv("HOME", proof)
	t.Setenv("HERMES_HOME", filepath.Join(proof, "must-not-use-hermes-home"))
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	revision := strings.Repeat("a", 40)
	first, second := filepath.Join(proof, "first.tar.gz"), filepath.Join(proof, "second.tar.gz")
	digest, err := skillbundle.BuildArchive(root, first, "0.2.18", revision)
	if err != nil {
		t.Fatal(err)
	}
	digest2, err := skillbundle.BuildArchive(root, second, "0.2.19", revision)
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(proof, "explicit-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, step := range []struct{ verb, archive, digest, version string }{
		{"install", first, digest, "0.2.18"}, {"update", second, digest2, "0.2.19"}, {"rollback", first, digest, "0.2.18"},
	} {
		got, err := runSkills(t, ctx, "skills", step.verb, step.archive, step.digest, revision, home)
		if err != nil || got.Version != step.version || got.ArchiveDigest != step.digest || got.SourceRevision != revision || got.AuthorityGranted || len(got.Skills) != 15 || got.EvidenceClass != "installed_inventory_verification" {
			t.Fatalf("%s: %+v %v", step.verb, got, err)
		}
		// Real absent configuration on first install, then invalid ambient YAML.
		if err := os.MkdirAll(filepath.Join(proof, ".aegis"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proof, ".aegis", "aegis.yaml"), []byte("invalid: [yaml"), 0600); err != nil {
			t.Fatal(err)
		}
		inventory, err := runSkills(t, ctx, "skills", "inventory", home)
		if err != nil || !reflect.DeepEqual(got, inventory) {
			t.Fatalf("inventory: %+v %v", inventory, err)
		}
	}
	for _, bad := range [][]string{
		{first, "invalid", revision, home}, {first, digest, "main", home},
		{first, "sha256:" + strings.Repeat("0", 64), revision, home},
		{first, digest, strings.Repeat("b", 40), home},
		{first, digest, revision, "."}, {first, digest, revision, filepath.Join(proof, "absent")},
	} {
		if _, err := runSkills(t, ctx, append([]string{"skills", "install"}, bad...)...); err == nil {
			t.Fatalf("accepted invalid input: %v", bad)
		}
	}
	for _, destination := range []string{".", filepath.Join(proof, "absent")} {
		if _, err := runSkills(t, ctx, "skills", "inventory", destination); err == nil {
			t.Fatalf("accepted inventory destination: %s", destination)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := runSkills(t, cancelled, "skills", "update", second, digest2, revision, home); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not propagated: %v", err)
	}
	got, err := runSkills(t, ctx, "skills", "inventory", home)
	if err != nil || got.ArchiveDigest != digest {
		t.Fatalf("denials changed inventory: %+v %v", got, err)
	}
	local := filepath.Join(home, "skills", "aegis", "SKILL.md")
	data, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("\nLocal edit must survive.\n")...)
	if err := os.WriteFile(local, data, 0644); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"inventory", "install", "update", "rollback"} {
		args := []string{"skills", verb, home}
		if verb != "inventory" {
			args = []string{"skills", verb, second, digest2, revision, home}
		}
		if _, err := runSkills(t, ctx, args...); err == nil {
			t.Fatalf("%s accepted local edits", verb)
		}
	}
	after, err := os.ReadFile(local)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatalf("local edit lost: %v", err)
	}
	for _, path := range []string{filepath.Join(proof, "must-not-use-hermes-home"), filepath.Join(home, "state"), filepath.Join(home, "persistence")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("unexpected state: %s %v", path, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(proof, ".aegis"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "aegis.yaml" {
		t.Fatalf("ambient state created: %v %v", entries, err)
	}
}
