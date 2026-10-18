//go:build linux

package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/core"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/testprocess"
)

// This is the real serve/default pinned-companion boundary, not an injected
// launcher or a direct invocation of the hidden companion. No human is approved.
func TestInstalledDaemonDoerProtectedSetupNoDisplay(t *testing.T) {
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(home, repo)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Skip("protected daemon development fixture requires a checkout under the operator home")
	}
	for path := repo; ; path = filepath.Dir(path) {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0022 != 0 {
			t.Skipf("pinned companion requires non-group/other-writable checkout ancestry: %s (harden before running this acceptance test)", path)
		}
		if path == "/" {
			break
		}
	}
	scratch := filepath.Join(repo, ".t")
	if err = os.MkdirAll(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(scratch, "p")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	// The executable must be in the actual module/Git root for development layout
	// resolution, and every ancestor must pass the production pinned-path guard.
	binary := filepath.Join(repo, ".protected-test-"+filepath.Base(root))
	t.Cleanup(func() { os.Remove(binary) })
	build := exec.Command("go", "build", "-p=1", "-ldflags=-X github.com/berryhill/aegis/internal/buildinfo.Version=dev", "-o", binary, "./cmd/aegis")
	build.Dir = repo
	if out, err := testprocess.CombinedOutput(build, 2*time.Minute); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	if err := os.Chmod(binary, 0700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(root, "owner.json")
	socket := filepath.Join(root, "s")
	if len(socket) >= 108 {
		t.Fatalf("repository-local Unix socket too long: %s", socket)
	}
	var draft app.DoerDraft
	var svc *app.Service
	var subject core.Subject
	var origin string
	// Subtest cleanup closes BOTH Badger stores before transferring ownership to
	// serve. The root belongs to the parent, so child cleanup cannot remove it.
	if !t.Run("enrolled-fixture", func(t *testing.T) {
		svc = apiServiceAt(t, root)
		workspace := filepath.Join(root, "work")
		if err := os.Mkdir(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		_, subject, _, _, draft = nativeSetupFixtureAt(t, svc, workspace)
		configurePreparationHTTP(t, svc)
		svc.Config.API.UnixSocket = socket
		origin = svc.Config.API.Console.Origin
		raw, err := json.Marshal(svc.Config)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(configFile, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}) {
		t.Fatal("fixture failed")
	}
	log, err := os.Create(filepath.Join(root, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	daemon := exec.Command(binary, "--config", configFile, "serve")
	daemon.Env = []string{"HOME=" + os.Getenv("HOME"), "PATH=" + os.Getenv("PATH"), "LANG=C"}
	daemon.Stdout, daemon.Stderr = log, log
	if err = daemon.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = daemon.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- daemon.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("daemon exit: %v", err)
			}
		case <-time.After(10 * time.Second):
			_ = daemon.Process.Kill()
			<-done
			t.Error("daemon shutdown timed out")
		}
	}
	t.Cleanup(stop)
	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, e := net.DialTimeout("unix", socket, 50*time.Millisecond)
		if e == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(filepath.Join(root, "daemon.log"))
			t.Fatalf("daemon readiness: %v %s", e, data)
		}
		time.Sleep(30 * time.Millisecond)
	}
	cli := &installedLoopQueueClient{binary: binary, config: configFile, target: origin, root: root}
	for _, action := range []string{"successor", "host", "provision"} {
		input := app.DoerProtectedSetupInput{ID: draft.ID, ExpectedVersion: draft.Version, Action: action}
		raw, _ := json.Marshal(input)
		file := filepath.Join(root, "request-"+action+".json")
		if err := os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
		var result app.DoerProtectedSetupResult
		if err := cli.run(&result, "loops", "setup-protected", file); err != nil {
			t.Fatal(err)
		}
		if result.Status != "unavailable" || !reflect.DeepEqual(result.Draft, draft) || result.Successor != nil || result.Host != nil || result.Provisioning != nil {
			t.Fatalf("%s changed authority/draft or missed no-display boundary: %+v", action, result)
		}
	}
	// Independent public readback, then clean stop/reopen to inspect durable facts.
	var retained app.DoerDraft
	if err := cli.run(&retained, "loops", "draft-show", draft.ID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retained, draft) {
		t.Fatal("public draft readback changed")
	}
	stop()
	authority, err := authoritybadger.Open(context.Background(), filepath.Join(svc.Config.StateDir, "persistence", "authority-v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	svc.Authority, svc.AuthorityCommands = authority, authority
	configureAPIFleet(t, svc)
	got, err := svc.ReadDoerDraftAs(context.Background(), subject, draft.ID)
	if err != nil || !reflect.DeepEqual(got, draft) {
		t.Fatalf("durable draft changed: %v", err)
	}
	charters, err := svc.ListCharters(draft.Agent.ID)
	if err != nil || len(charters) != 1 {
		t.Fatalf("successor imported: %v", err)
	}
	agent, err := svc.GetFleetAgentAs(context.Background(), subject, draft.Agent.ID, 0)
	if err != nil || agent.Revision.Revision != draft.Agent.Revision || agent.Revision.Digest != draft.Agent.Digest {
		t.Fatalf("registered Agent changed: %v", err)
	}
	approvals, err := svc.ListApprovals()
	if err != nil || len(approvals) != 0 {
		t.Fatalf("approval manufactured: %v", err)
	}
	receipts, err := svc.ListReceipts()
	if err != nil || len(receipts) != 0 {
		t.Fatalf("provisioning manufactured: %v", err)
	}
	// Resolver denial also covers corruption and I/O failures; it is not proof
	// of absence. Inspect the fixture's durable record directory explicitly.
	entries, err := os.ReadDir(filepath.Join(root, "state", "doer-host-approvals"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("cannot inspect durable host grants: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("host approval records manufactured: %d", len(entries))
	}
	t.Log("real daemon + default pinned companion: successor/host/provision unavailable; exact durable draft, Agent and authority unchanged")
}
