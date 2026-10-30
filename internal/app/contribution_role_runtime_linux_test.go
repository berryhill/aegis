//go:build linux

package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/testprocess"
	"golang.org/x/sys/unix"
)

// A private subreaper makes any orphaned fixture descendant observable instead
// of relying on the host's PID 1 reaping speed. Production stop admission and
// its original four-second bound remain the implementation under test.
func TestContributionRoleFixtureStopsWithoutHostReaper(t *testing.T) {
	const marker = "AEGIS_ROLE_FIXTURE_REAPER_CHILD"
	mode, present := os.LookupEnv(marker)
	if present && mode != "1" {
		t.Fatal("invalid role fixture reaper child mode")
	}
	if !present {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(executable, "-test.run=^TestContributionRoleFixtureStopsWithoutHostReaper$", "-test.count=1", "-test.v", "-test.timeout=15s")
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), marker + "=1", "GOMAXPROCS=" + strconv.Itoa(runtime.GOMAXPROCS(0))}
		if memory := os.Getenv("GOMEMLIMIT"); memory != "" {
			cmd.Env = append(cmd.Env, "GOMEMLIMIT="+memory)
		}
		output, err := testprocess.CombinedOutput(cmd, 15*time.Second)
		if err != nil {
			t.Fatalf("role fixture stop regression: %v\n%s", err, output)
		}
		return
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if group, err := unix.Getpgid(0); err != nil || group != os.Getpid() {
		t.Fatal("subreaper regression requires its isolated owned process group")
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	defer unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 0, 0, 0, 0)
	s, _, subject, _, _ := contributionAuthorityFixture(t)
	ctx := context.Background()
	mandate, _, err := s.PreviewSessionAs(ctx, subject, "contribution-builder", 1, "principal", core.Environment{Name: "local"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.StartSessionAs(ctx, subject, mandate.ID)
	if err != nil {
		t.Fatal(err)
	}
	descendant, descendantFD := 0, -1
	defer func() {
		_ = s.Hermes.Terminate(context.Background(), session.RuntimeSessionID, true)
		if descendantFD >= 0 {
			_ = unix.PidfdSendSignal(descendantFD, unix.SIGKILL, nil, 0)
			var status unix.WaitStatus
			_, _ = unix.Wait4(descendant, &status, 0, nil)
			_ = unix.Close(descendantFD)
		}
	}()

	ready := filepath.Join(session.RuntimeHome, "child.pid")
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for descendant == 0 {
		if raw, readErr := os.ReadFile(ready); readErr == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if parseErr == nil && pid > 0 {
				descendant = pid
				break
			}
		}
		select {
		case <-tick.C:
		case <-timer.C:
			t.Fatal("role fixture did not publish its owned process readiness")
		}
	}
	if descendant != session.RuntimePID {
		fd, err := unix.PidfdOpen(descendant, 0)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(descendant) + "/stat")
		if err != nil {
			_ = unix.Close(fd)
			t.Fatal(err)
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			_ = unix.Close(fd)
			t.Fatal("malformed fixture descendant identity")
		}
		fields := strings.Fields(string(raw[end+1:]))
		if len(fields) < 3 || fields[1] != strconv.Itoa(session.RuntimePID) || fields[2] != strconv.Itoa(session.RuntimePID) {
			_ = unix.Close(fd)
			t.Fatal("fixture descendant is not owned by the exact runtime leader/group")
		}
		leader, err := os.ReadFile("/proc/" + strconv.Itoa(session.RuntimePID) + "/stat")
		if err != nil {
			_ = unix.Close(fd)
			t.Fatal(err)
		}
		leaderEnd := strings.LastIndexByte(string(leader), ')')
		if leaderEnd < 0 {
			_ = unix.Close(fd)
			t.Fatal("malformed fixture leader identity")
		}
		leaderFields := strings.Fields(string(leader[leaderEnd+1:]))
		if len(leaderFields) <= 19 || leaderFields[19] != session.ProcessStart {
			_ = unix.Close(fd)
			t.Fatal("fixture leader identity changed before descendant custody")
		}
		descendantFD = fd
	}
	authority, err := s.authorityContextForSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	binding := hermes.ContributionStopBinding{SessionID: session.ID, RuntimeSessionID: session.RuntimeSessionID, AuthorityID: authority.ID, AuthorityDigest: authority.Digest, Role: "builder", Epoch: 1, PID: session.RuntimePID, ProcessStart: session.ProcessStart}
	proof, err := s.Hermes.StopContributionRole(ctx, binding)
	if err != nil || !proof.RoleProcessAcknowledged() || proof.Binding() != binding {
		t.Fatalf("fixture required host orphan reaping before exact stop acknowledgement: %v", err)
	}
}
