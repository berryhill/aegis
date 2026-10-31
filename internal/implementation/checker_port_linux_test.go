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
	"strings"
	"testing"
)

func TestCheckCandidateMissingNativePortDeniesBeforeStart(t *testing.T) {
	e, c := fixture(t)
	e.Native = nil
	marker := filepath.Join(t.TempDir(), "started")
	e.GoBinary = filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(e.GoBinary, []byte(fmt.Sprintf("#!/bin/sh\n/bin/touch %q\n", marker)), 0700); err != nil {
		t.Fatal(err)
	}
	if output, passed, err := e.checkCandidate(context.Background(), c, nil); err == nil || passed || len(output) != 0 {
		t.Fatalf("missing port accepted: passed=%v output=%s err=%v", passed, output, err)
	}
	proposed := false
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { proposed = true; return edits("42"), nil })
	if _, err := e.Run(context.Background(), "missing-native", c); err == nil || proposed {
		t.Fatalf("missing port reached proposal: proposed=%v err=%v", proposed, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("checker started without qualified port: %v", err)
	}
}

func TestCheckCandidateKnownFailureReleasesCustody(t *testing.T) {
	for _, cause := range []error{errors.New("fixture start failure"), context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			e, c := fixture(t)
			e.GoBinary = filepath.Join(runtime.GOROOT(), "bin", "go")
			e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { return edits("42"), nil })
			var home string
			e.Native = func(_ context.Context, cmd *exec.Cmd) error {
				for _, value := range cmd.Env {
					if strings.HasPrefix(value, "HOME=") {
						home = strings.TrimPrefix(value, "HOME=")
					}
				}
				return cause // Known no-start fixture: no writers to join.
			}
			r, err := e.Run(context.Background(), "known-checker", c)
			if !errors.Is(err, cause) || r.State == "succeeded" || home == "" {
				t.Fatalf("known failure: state=%s err=%v home=%q", r.State, err, home)
			}
			if _, err := os.Stat(home); !os.IsNotExist(err) {
				t.Fatalf("known checker home retained: %v", err)
			}
			unlock, err := lockWorkspace(c.Workspace)
			if err != nil {
				t.Fatalf("known failure retained workspace: %v", err)
			}
			unlock()
		})
	}
}
