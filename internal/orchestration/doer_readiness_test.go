package orchestration

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/registry"
)

func TestDoerReadinessDeniesMissingPrerequisites(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	home := filepath.Join(root, "laya-home")
	for _, path := range []string{workspace, home} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	python := filepath.Join(root, "python")
	if err := os.WriteFile(python, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBinary, err = filepath.EvalSymlinks(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	contract := loop.DoerContract{Task: "Update selected file", Workspace: workspace, WritableFiles: []string{"value.go"}, VerifyFile: "value.go", MaxAttempts: 1}
	revision, _, err := loop.NewDoerRevision("doer", 1, "", contract)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := contract.Digest()
	if err != nil {
		t.Fatal(err)
	}
	adapter := routedHermesTestAdapter(t, root, "#!/bin/sh\nexit 0\n")
	agent := routedRuntimeRequest(root).Participant
	configured := func() *QueueWorker {
		return &QueueWorker{implementation: &ImplementationController{
			config: config.Implementation{GoBinary: goBinary, LayaPython: python, LayaHome: home, AuthorizedContracts: []string{digest}},
			root:   filepath.Join(root, "state", "persistence", "fleet-v1"), adapter: adapter.hermes,
			decision: NewLayaDecisionAdapter(LocalLayaProcess{PythonExecutable: python, Home: home}),
		}}
	}
	if err := configured().ValidateLoopAdmission(revision, agent); err != nil {
		t.Fatalf("configured readiness: %v", err)
	}
	if err := configured().ValidateDoerAvailability(context.Background(), revision, agent); err == nil {
		t.Fatal("silent local Laya executable was mistaken for a working helper")
	}
	responding := configured()
	responding.implementation.decision = NewLayaDecisionAdapter(fakeLayaProcess(func(context.Context, []byte) ([]byte, error) {
		return []byte(`{"version":1,"kind":"gate","answers":{"specified":{"choice":"no","answer_confidence":0.9},"result_defined":{"choice":"no","answer_confidence":0.9}}}`), nil
	}))
	if err := responding.ValidateDoerAvailability(context.Background(), revision, agent); err != nil {
		t.Fatalf("a typed negative verdict must not be confused with missing local Laya: %v", err)
	}
	tests := []struct {
		name   string
		change func(*QueueWorker)
		want   string
	}{
		{"no-controller", func(w *QueueWorker) { w.implementation = nil }, "doer_controller_unavailable"},
		{"not-authorized", func(w *QueueWorker) { w.implementation.config.AuthorizedContracts = nil }, "doer_host_approval_required"},
		{"no-laya", func(w *QueueWorker) { w.implementation.config.LayaPython = "" }, "doer_controller_unavailable"},
		{"no-home", func(w *QueueWorker) { w.implementation.config.LayaHome = "" }, "doer_controller_unavailable"},
		{"no-checker", func(w *QueueWorker) { w.implementation.config.GoBinary = "" }, "doer_go_executable_untrusted"},
		{"no-adapter", func(w *QueueWorker) { w.implementation.adapter = nil }, "doer_controller_unavailable"},
		{"untrusted-laya", func(w *QueueWorker) { w.implementation.config.LayaPython = filepath.Join(root, "absent") }, "doer_laya_executable_untrusted"},
		{"untrusted-home", func(w *QueueWorker) { w.implementation.config.LayaHome = filepath.Join(root, "missing-home") }, "doer_laya_home_untrusted"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := configured()
			tc.change(w)
			if got := DoerPrerequisiteReason(w.ValidateLoopAdmission(revision, agent)); got != tc.want {
				t.Fatalf("earliest cause %q want %q", got, tc.want)
			}
		})
	}
	for _, cause := range []error{hostapproval.ErrAbsent, hostapproval.ErrDenied, hostapproval.ErrExpired, errors.New("secret=/private/token")} {
		w := configured()
		w.implementation.config.AuthorizedContracts = nil
		w.implementation.config.GoBinary = filepath.Join(root, "missing-downstream-checker")
		w.SetDoerHostApprovalResolver(func(context.Context, loop.DoerContract, registry.AgentRevision) error { return cause })
		if got, want := DoerPrerequisiteReason(w.ValidateDoerContractReadiness(context.Background(), contract, agent, true)), DoerPrerequisiteReason(cause); got != want {
			t.Fatalf("resolver cause %q want %q", got, want)
		}
	}
	t.Run("symlink-target", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(workspace, "other.go"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("other.go", filepath.Join(workspace, "value.go")); err != nil {
			t.Fatal(err)
		}
		if err := configured().ValidateLoopAdmission(revision, agent); err == nil {
			t.Fatal("symlink target admitted")
		}
	})
}

func TestTrustedExecutableRejectsWritableToolchain(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(binary, []byte("not executed"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0775, 0757, 0777} {
		if err := os.Chmod(binary, mode); err != nil {
			t.Fatal(err)
		}
		if err := trustedExecutable(binary); err == nil {
			t.Fatalf("writable executable admitted: %o", mode)
		}
	}
	if err := os.Chmod(binary, 0755); err != nil {
		t.Fatal(err)
	}
	if err := trustedExecutable(binary); err != nil {
		t.Fatalf("hardened executable rejected: %v", err)
	}
}

func TestDoerWorkspacePolicyRejectsExistingHardlink(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "value.go")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, filepath.Join(workspace, "other.go")); err != nil {
		t.Fatal(err)
	}
	contract := loop.DoerContract{Task: "Update selected file", Workspace: workspace, WritableFiles: []string{"value.go"}, VerifyFile: "value.go", MaxAttempts: 1}
	if err := doerWorkspacePolicy(contract); err == nil {
		t.Fatal("existing hardlink admitted before activation")
	}
}
