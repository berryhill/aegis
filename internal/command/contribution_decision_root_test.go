package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Run the actual constructor-built hidden command in a child so fd 3 is real
// and never collides with the test runner's own descriptors.
func TestContributionDecisionRootChild(t *testing.T) {
	if os.Getenv("AEGIS_CONTRIBUTION_ROOT_CHILD") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("AEGIS_CONTRIBUTION_ROOT_ARGS")), &args); err != nil {
		panic(err)
	}
	root := NewRoot(Dependencies{In: strings.NewReader(""), Out: os.Stdout, Err: os.Stderr, Version: "dev", Profile: DevelopmentProfile, DevelopmentRoot: os.Getenv("AEGIS_CONTRIBUTION_ROOT_REPO"), IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func TestContributionDecisionRootIPCPreflight(t *testing.T) {
	for _, tc := range []struct {
		name, want   string
		config, pipe bool
		extra        []string
		fail         bool
	}{
		{name: "missing-config", want: "policy_rejected", pipe: true},
		{name: "valid-config-no-fd", config: true, want: "transport_unavailable"},
		{name: "valid-config-no-display", config: true, pipe: true, want: "unavailable"},
		{name: "origin-mismatch", config: true, pipe: true, extra: []string{"--origin", "http://127.0.0.1:8451"}, want: "policy_rejected"},
		{name: "target-rejected", config: true, pipe: true, extra: []string{"--target", "http://127.0.0.1:8451"}, fail: true},
		{name: "production-profile-path-rejected", config: true, pipe: true, extra: []string{"--config", "PRODUCTION"}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			repo := filepath.Join(home, "repository")
			if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module github.com/berryhill/aegis\n\ngo 1.26\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := filepath.Join(repo, "isolated.yaml")
			socket := filepath.Join(repo, "absent.sock")
			if tc.config {
				data := fmt.Sprintf("state_dir: %s\nprincipal:\n  id: test\n  name: Test\n  uid: %q\n  user: fixture\napi:\n  token: fixture-only-canary\n  unix_socket: %s\n  listen: 127.0.0.1:8450\n  console:\n    origin: http://127.0.0.1:8450\naudit:\n  checkpoint_dir: %s\n", filepath.Join(repo, "state"), fmt.Sprint(os.Getuid()), socket, filepath.Join(repo, "checkpoints"))
				if err := os.WriteFile(cfg, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := func() map[string]string {
				result := map[string]string{}
				err := filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					info, err := d.Info()
					if err != nil {
						return err
					}
					value := info.Mode().String()
					if !d.IsDir() {
						b, err := os.ReadFile(path)
						if err != nil {
							return err
						}
						value += string(b)
					}
					result[path] = value
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			before := snapshot()
			args := []string{"--config", cfg, "contribution-decision-companion", "--socket", socket, "--origin", "http://127.0.0.1:8450", "--key", "key", "--request-id", "intent", "--role", "builder"}
			for _, a := range tc.extra {
				if a == "PRODUCTION" {
					a = filepath.Join(home, ".aegis", "aegis.yaml")
				}
				args = append(args, a)
			}
			raw, _ := json.Marshal(args)
			child := exec.Command(os.Args[0], "-test.run=^TestContributionDecisionRootChild$")
			child.Env = append(os.Environ(), "AEGIS_CONTRIBUTION_ROOT_CHILD=1", "AEGIS_CONTRIBUTION_ROOT_ARGS="+string(raw), "AEGIS_CONTRIBUTION_ROOT_REPO="+repo, "HOME="+home, "DISPLAY=", "WAYLAND_DISPLAY=")
			if tc.pipe {
				read, write, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer read.Close()
				if _, err = write.WriteString("fixture-only-canary"); err != nil {
					t.Fatal(err)
				}
				write.Close()
				child.ExtraFiles = []*os.File{read}
			}
			out, err := child.CombinedOutput()
			if tc.fail {
				if err == nil {
					t.Fatalf("forbidden root arguments accepted: %s", out)
				}
			} else {
				if err != nil {
					t.Fatalf("root command: %v: %s", err, out)
				}
				var result struct {
					Status string `json:"status"`
				}
				if err := json.Unmarshal(out, &result); err != nil || result.Status != tc.want {
					t.Fatalf("want %s, got %s (%v)", tc.want, out, err)
				}
			}
			if bytes.Contains(out, []byte("fixture-only-canary")) {
				t.Fatal("bearer leaked")
			}
			if !reflect.DeepEqual(before, snapshot()) {
				t.Fatal("IPC command mutated isolated files or opened stores")
			}
		})
	}
	root := NewRoot(Dependencies{Out: io.Discard, Err: io.Discard})
	cmd, _, err := root.Find([]string{"contribution-decision-companion"})
	if err != nil || !cmd.Hidden {
		t.Fatal("companion is not hidden")
	}
}
