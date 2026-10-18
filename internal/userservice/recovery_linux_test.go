package userservice

import (
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestRecovery(t *testing.T) {
	for _, scenario := range []string{"positive", "tamper", "inode", "symlink", "running", "pid", "confirmation", "evidence", "reload"} {
		t.Run(scenario, func(t *testing.T) {
			executable, configPath := serviceFixture(t)
			if err := os.Chmod(filepath.Dir(configPath), 0700); err != nil {
				t.Fatal(err)
			}
			plan, err := Preview(executable, configPath)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Dir(plan.UnitPath), 0700); err != nil {
				t.Fatal(err)
			}
			foreign := []byte("# foreign\nExecStart=/fixture/aegis\n")
			if err = os.WriteFile(plan.UnitPath, foreign, 0600); err != nil {
				t.Fatal(err)
			}
			runner := &recordingRunner{mainPID: "0", fragmentPath: plan.UnitPath, execStart: "{ path=" + executable + " ; argv[]=" + executable + " serve --config " + configPath + " ; ignore_errors=no ; }"}
			preview, err := PreviewRecovery(context.Background(), runner, plan)
			if err != nil {
				t.Fatal(err)
			}
			confirmation := preview.Confirmation
			switch scenario {
			case "tamper":
				os.WriteFile(plan.UnitPath, []byte("changed"), 0600)
			case "inode":
				os.Rename(plan.UnitPath, plan.UnitPath+".old")
				os.WriteFile(plan.UnitPath, foreign, 0600)
			case "symlink":
				os.Remove(plan.UnitPath)
				os.Symlink(executable, plan.UnitPath)
			case "running":
				runner.activeState = "active"
			case "pid":
				runner.mainPID = "42"
			case "confirmation":
				confirmation = "yes"
			case "evidence":
				os.Mkdir(preview.EvidencePath, 0700)
			case "reload":
				runner.runErrors = map[string]error{"daemon-reload": errors.New("reload failed")}
			}
			err = ApplyRecovery(context.Background(), runner, preview, confirmation)
			if scenario != "positive" {
				if err == nil {
					t.Fatal("unsafe recovery accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(plan.UnitPath)
			if string(got) != string(plan.unit) {
				t.Fatal("unit mismatch")
			}
			got, _ = os.ReadFile(preview.EvidencePath)
			if string(got) != string(foreign) {
				t.Fatal("foreign evidence lost")
			}
			if _, err = PreviewRecovery(context.Background(), runner, plan); err == nil {
				t.Fatal("repeat must not replace exact unit")
			}
		})
	}
}

type overrideRecoveryRunner struct {
	*recordingRunner
	override    string
	afterReload bool
	reloaded    bool
}

func (r *overrideRecoveryRunner) Output(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) > 3 && args[3] == "DropInPaths" {
		if !r.afterReload || r.reloaded {
			return []byte(r.override), nil
		}
		return nil, nil
	}
	return r.recordingRunner.Output(ctx, args...)
}
func (r *overrideRecoveryRunner) Run(ctx context.Context, args ...string) error {
	if len(args) > 0 && args[0] == "daemon-reload" {
		r.reloaded = true
	}
	return r.recordingRunner.Run(ctx, args...)
}
func TestRecoveryRejectsOverrides(t *testing.T) {
	for _, post := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "after"}[post], func(t *testing.T) {
			exe, cfg := serviceFixture(t)
			os.Chmod(filepath.Dir(cfg), 0700)
			p, e := Preview(exe, cfg)
			if e != nil {
				t.Fatal(e)
			}
			os.MkdirAll(filepath.Dir(p.UnitPath), 0700)
			os.WriteFile(p.UnitPath, []byte("foreign"), 0600)
			r := &overrideRecoveryRunner{recordingRunner: &recordingRunner{mainPID: "0", fragmentPath: p.UnitPath, execStart: "{ path=" + exe + " ; argv[]=" + exe + " serve --config " + cfg + " ; ignore_errors=no ; }"}, override: "/override.conf", afterReload: post}
			preview, e := PreviewRecovery(context.Background(), r, p)
			if !post {
				if e == nil {
					t.Fatal("override accepted before mutation")
				}
				b, _ := os.ReadFile(p.UnitPath)
				if string(b) != "foreign" {
					t.Fatal("mutated")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if ApplyRecovery(context.Background(), r, preview, preview.Confirmation) == nil {
				t.Fatal("post reload override accepted")
			}
		})
	}
}
func TestRecoveryAncestryPolicy(t *testing.T) {
	for _, scenario := range []string{"private775", "accessible775", "symlink", "foreign", "acl"} {
		t.Run(scenario, func(t *testing.T) {
			root, err := os.MkdirTemp("", "aegis-recovery-access-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(root) })
			os.Chmod(root, 0700)
			child := filepath.Join(root, "child")
			os.Mkdir(child, 0775)
			os.Chmod(child, 0775)
			switch scenario {
			case "accessible775":
				os.Chmod(root, 0755)
			case "symlink":
				os.Rename(child, child+"real")
				os.Symlink(child+"real", child)
			case "foreign":
				if os.Geteuid() != 0 {
					t.Skip("foreign chown fixture requires privileged test runner")
				}
				if e := os.Chown(child, 12345, 12345); e != nil {
					t.Fatal(e)
				}
			case "acl":
				acl := []byte{2, 0, 0, 0, 1, 0, 7, 0, 255, 255, 255, 255, 2, 0, 5, 0, 57, 48, 0, 0, 4, 0, 0, 0, 255, 255, 255, 255, 16, 0, 5, 0, 255, 255, 255, 255, 32, 0, 0, 0, 255, 255, 255, 255}
				if e := unix.Setxattr(child, "system.posix_acl_access", acl, 0); e != nil {
					t.Fatalf("ACL fixture unavailable: %v", e)
				}
			}
			d, e := recoveryDirectory(child)
			if d != nil {
				d.Close()
			}
			if scenario == "private775" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("unsafe ancestry accepted")
			}
		})
	}
}
func TestRecoveryRejectsUnsafeExecutable(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "writable"} {
		t.Run(kind, func(t *testing.T) {
			exe, cfg := serviceFixture(t)
			os.Chmod(filepath.Dir(cfg), 0700)
			p, e := Preview(exe, cfg)
			if e != nil {
				t.Fatal(e)
			}
			os.MkdirAll(filepath.Dir(p.UnitPath), 0700)
			os.WriteFile(p.UnitPath, []byte("foreign"), 0600)
			switch kind {
			case "symlink":
				os.Rename(exe, exe+"real")
				os.Symlink(exe+"real", exe)
			case "hardlink":
				os.Link(exe, exe+"link")
			case "writable":
				os.Chmod(exe, 0777)
			}
			if _, e = PreviewRecovery(context.Background(), &recordingRunner{mainPID: "0"}, p); e == nil {
				t.Fatal("unsafe executable accepted")
			}
		})
	}
}
