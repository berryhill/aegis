//go:build linux

package implementation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

// Exercise the production caller, not configureProcess in isolation. A single
// stat observation immediately after return must already show no live writer;
// waiting after return would hide the bug this test guards.
func TestCheckCandidateReturnQuiescence(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		dir := t.TempDir()
		pidfile := filepath.Join(dir, "pids")
		checker := filepath.Join(dir, "go")
		script := "#!/bin/sh\nexec 1>/dev/null 2>/dev/null\n"
		for i := 0; i < 64; i++ {
			script += fmt.Sprintf("(while :; do :; done) & echo $! >> %q\n", pidfile)
		}
		script += "exit 0\n"
		if err := os.WriteFile(checker, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		c := loop.ImplementationDraft("checker", "quiescent return")
		c.Workspace = dir
		c.Policy.TimeoutSeconds = 10
		e := &Executor{GoBinary: checker, Native: runCheckerNative}
		_, _, err := e.checkCandidate(context.Background(), c, nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(pidfile)
		if err != nil {
			t.Fatal(err)
		}
		for _, pid := range strings.Fields(string(raw)) {
			if _, err := strconv.Atoi(pid); err != nil {
				t.Fatal(err)
			}
			stat, err := os.ReadFile("/proc/" + pid + "/stat")
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			fields := strings.Fields(string(stat)[strings.LastIndexByte(string(stat), ')')+1:])
			if len(fields) == 0 || (fields[0] != "Z" && fields[0] != "X" && fields[0] != "x") {
				t.Fatalf("attempt %d: checkCandidate returned before descendant %s quiesced: %s", attempt, pid, stat)
			}
		}
	}
}
