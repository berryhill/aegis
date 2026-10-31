//go:build linux

package implementation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/loop"
)

func TestCheckCandidateNativeOutcomes(t *testing.T) {
	for _, mode := range []string{"success", "failed-exit", "inherited-pipe", "cancel", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			pidfile := filepath.Join(dir, "pid")
			homefile := filepath.Join(dir, "home")
			checker := filepath.Join(dir, "go")
			script := fmt.Sprintf("#!/bin/sh\necho \"$HOME\" > %q\n", homefile)
			descendant := mode == "inherited-pipe" || mode == "cancel" || mode == "deadline"
			if descendant {
				script += fmt.Sprintf("/bin/sleep 60 & echo $! > %q\n", pidfile)
			}
			if mode == "cancel" || mode == "deadline" {
				script += "wait\n"
			}
			script += "printf '%s\\n' '{\"Action\":\"run\",\"Package\":\"fixture\",\"Test\":\"TestCheck\"}' '{\"Action\":\"pass\",\"Package\":\"fixture\",\"Test\":\"TestCheck\"}'\n"
			if mode == "failed-exit" {
				script += "exit 7\n"
			}
			if err := os.WriteFile(checker, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if mode == "deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 200*time.Millisecond)
				defer stop()
			}
			cancelDone := make(chan struct{})
			if mode == "cancel" {
				go func() {
					defer close(cancelDone)
					for {
						if _, err := os.Stat(pidfile); err == nil {
							cancel()
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(time.Millisecond):
						}
					}
				}()
			}
			c := loop.ImplementationDraft("checker", "native outcomes")
			c.Workspace = dir
			c.Policy.TimeoutSeconds = 10
			c.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "fixture", Name: "TestCheck"}}
			e := &Executor{GoBinary: checker, Native: runCheckerNative}
			output, passed, err := e.checkCandidate(ctx, c, nil)
			if mode == "cancel" {
				<-cancelDone
			}
			switch mode {
			case "cancel":
				if !errors.Is(err, context.Canceled) || passed {
					t.Fatalf("cancel: passed=%v err=%v", passed, err)
				}
			case "deadline":
				if !errors.Is(err, context.DeadlineExceeded) || passed {
					t.Fatalf("deadline: passed=%v err=%v", passed, err)
				}
			default:
				if err != nil || passed != (mode != "failed-exit") || len(output) == 0 {
					t.Fatalf("outcome: passed=%v output=%s err=%v", passed, output, err)
				}
			}
			if descendant {
				raw, readErr := os.ReadFile(pidfile)
				if readErr != nil {
					t.Fatalf("descendant fixture did not start: %v", readErr)
				}
				pid := strings.TrimSpace(string(raw))
				stat, statErr := os.ReadFile("/proc/" + pid + "/stat")
				if statErr != nil && !os.IsNotExist(statErr) {
					t.Fatal(statErr)
				}
				if statErr == nil && !strings.Contains(string(stat), ") Z ") && !strings.Contains(string(stat), ") X ") && !strings.Contains(string(stat), ") x ") {
					t.Fatalf("live descendant at return: %s", stat)
				}
			}
			home, readErr := os.ReadFile(homefile)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if _, statErr := os.Stat(strings.TrimSpace(string(home))); !os.IsNotExist(statErr) {
				t.Fatalf("joined checker home not removed: %v", statErr)
			}
		})
	}
}

type checkerFailureStore struct {
	Store
	fail bool
}

func (s *checkerFailureStore) Put(k, v []byte) error {
	if s.fail {
		return errors.New("fixture persistence unavailable")
	}
	return s.Store.Put(k, v)
}

func TestCheckCandidateUnknownRetainsCustody(t *testing.T) {
	for _, persistenceFailure := range []bool{false, true} {
		t.Run(strconv.FormatBool(persistenceFailure), func(t *testing.T) {
			e, c := fixture(t)
			e.GoBinary = filepath.Join(runtime.GOROOT(), "bin", "go")
			e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { return edits("42"), nil })
			store := &checkerFailureStore{Store: e.DB}
			e.DB = store
			var home string
			e.Native = func(_ context.Context, cmd *exec.Cmd) error {
				for _, value := range cmd.Env {
					if strings.HasPrefix(value, "HOME=") {
						home = strings.TrimPrefix(value, "HOME=")
					}
				}
				store.fail = persistenceFailure
				return errors.Join(context.Canceled, ErrCheckerQuiescenceUnavailable)
			}
			// Only the injected fixture owns these resources; no process started.
			// Production has no implicit quarantine-release path.
			t.Cleanup(func() {
				if unlock, ok := checkerQuarantine.LoadAndDelete(c.Workspace); ok {
					unlock.(func())()
				}
				if home != "" {
					_ = os.RemoveAll(home)
				}
			})
			r, err := e.Run(context.Background(), "unknown-checker", c)
			if err == nil || r.State == "succeeded" || home == "" {
				t.Fatalf("unknown accepted or fixture not exercised: state=%s err=%v home=%q", r.State, err, home)
			}
			if !persistenceFailure && !errors.Is(err, ErrCheckerQuiescenceUnavailable) {
				t.Fatal(err)
			}
			if _, err := os.Stat(home); err != nil {
				t.Fatalf("unknown checker home removed: %v", err)
			}
			unlock, err := lockWorkspace(c.Workspace)
			if err == nil {
				unlock()
				t.Fatal("unknown quiescence silently released workspace custody")
			}
			if !errors.Is(err, ErrWorkspaceBusy) {
				t.Fatal(err)
			}
		})
	}
}
