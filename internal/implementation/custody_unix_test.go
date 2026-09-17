//go:build linux

package implementation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceCustody(t *testing.T) {
	e, c := fixture(t)
	unlock, err := lockWorkspace(c.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { t.Fatal("concurrent proposal"); return nil, nil })
	if _, err = e.Run(context.Background(), "locked", c); err == nil {
		t.Fatal("competing custody accepted")
	}
	unlock()
	unlock, err = lockWorkspace(c.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}
func TestProcessGroupDescendantsTerminated(t *testing.T) {
	for _, cancelled := range []bool{true, false} {
		t.Run(strconv.FormatBool(cancelled), func(t *testing.T) {
			pidfile := filepath.Join(t.TempDir(), "pid")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			script := "sleep 60 & echo $! > " + pidfile + "; wait"
			if !cancelled {
				script = "sleep 60 & echo $! > " + pidfile
			}
			cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script)
			if err := configureProcess(cmd); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer killProcessGroup(cmd)
			var pid int
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				b, _ := os.ReadFile(pidfile)
				pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
				if pid > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if pid == 0 {
				t.Fatal("child never started")
			}
			if cancelled {
				cancel()
			}
			cmd.Wait()
			killProcessGroup(cmd)
			deadline = time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
				if os.IsNotExist(err) || strings.Contains(string(b), ") Z ") {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("descendant %d survived", pid)
		})
	}
}
