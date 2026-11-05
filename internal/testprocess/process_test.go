//go:build unix

package testprocess

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestObservedResultContextSnapshot(t *testing.T) {
	live, stopLive := context.WithCancel(context.Background())
	defer stopLive()
	expired, stopExpired := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer stopExpired()
	canceled, stopCanceled := context.WithCancelCause(context.Background())
	stopCanceled(errors.New("private-cause-canary"))
	killed := errors.New("signal: killed")
	output := []byte("private-output-canary")
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want OwnedContextState
	}{
		{"unobserved", nil, OwnedContextUnobserved},
		{"live", live, OwnedContextLive},
		{"expired", expired, OwnedContextDeadlineExceeded},
		{"canceled", canceled, OwnedContextCanceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := observedResult(tc.ctx, output, killed)
			if result.OwnedContext != tc.want {
				t.Fatalf("owned context=%d want=%d", result.OwnedContext, tc.want)
			}
			if result.Err != killed || !bytes.Equal(result.Output, output) {
				t.Fatal("observation changed the run error or private output")
			}
		})
	}
	result := observedResult(live, output, killed)
	stopLive() // Deferred cancellation/cleanup cannot rewrite the snapshot.
	if result.OwnedContext != OwnedContextLive {
		t.Fatal("post-run cancellation changed the live observation")
	}
}

func TestCombinedOutputObserved(t *testing.T) {
	lookupErr := errors.New("private-lookup-canary")
	badLookup := exec.Command("sh")
	badLookup.Err = lookupErr
	for _, tc := range []struct {
		name    string
		command *exec.Cmd
		timeout time.Duration
		want    OwnedContextState
		failed  bool
	}{
		{"nil", nil, time.Second, OwnedContextUnobserved, true},
		{"empty args", &exec.Cmd{}, time.Second, OwnedContextUnobserved, true},
		{"invalid timeout", exec.Command("sh"), 0, OwnedContextUnobserved, true},
		{"lookup failure", badLookup, time.Second, OwnedContextUnobserved, true},
		{"unsupported context", exec.CommandContext(context.Background(), "sh"), time.Second, OwnedContextUnobserved, true},
		{"success", exec.Command("sh", "-c", "printf private-output-canary"), time.Second, OwnedContextLive, false},
		{"unrelated kill", exec.Command("sh", "-c", "kill -KILL $$"), time.Second, OwnedContextLive, true},
		{"owned timeout", exec.Command("sh", "-c", "while :; do :; done"), 50 * time.Millisecond, OwnedContextDeadlineExceeded, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := CombinedOutputObserved(tc.command, tc.timeout)
			if result.OwnedContext != tc.want || (result.Err != nil) != tc.failed {
				t.Fatalf("state=%d failed=%t want state=%d failed=%t", result.OwnedContext, result.Err != nil, tc.want, tc.failed)
			}
			if tc.name == "lookup failure" && result.Err != lookupErr {
				t.Fatal("lookup error changed")
			}
			if tc.name == "success" && string(result.Output) != "private-output-canary" {
				t.Fatal("private output capture changed")
			}
		})
	}
}

func TestCommandSettingsContract(t *testing.T) {
	t.Run("extra files and argv zero", func(t *testing.T) {
		file, err := os.CreateTemp(t.TempDir(), "input")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if _, err := file.WriteString("descriptor"); err != nil {
			t.Fatal(err)
		}
		if _, err := file.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		command := exec.Command("sh", "-c", "printf '%s:' \"$0\"; cat <&3")
		command.Args[0] = "fixture-argv0"
		command.ExtraFiles = []*os.File{file}
		output, err := CombinedOutput(command, time.Second)
		if err != nil || string(output) != "fixture-argv0:descriptor" {
			t.Fatalf("output=%q err=%v", output, err)
		}
	})
	t.Run("context rejected", func(t *testing.T) {
		command := exec.CommandContext(context.Background(), "sh", "-c", "exit 0")
		if _, err := CombinedOutput(command, time.Second); err == nil {
			t.Fatal("context silently discarded")
		}
	})
	t.Run("process attributes rejected", func(t *testing.T) {
		command := exec.Command("sh", "-c", "exit 0")
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if _, err := CombinedOutput(command, time.Second); err == nil {
			t.Fatal("process attributes silently discarded")
		}
	})
}

func TestTimeoutAndOutputLimit(t *testing.T) {
	started := time.Now()
	if _, err := CombinedOutput(exec.Command("sh", "-c", "sleep 30 & wait"), 50*time.Millisecond); err == nil {
		t.Fatal("timeout accepted")
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("timeout failed to bound process")
	}
	var output boundedOutput
	if _, err := output.Write(make([]byte, MaxOutput+1)); err == nil {
		t.Fatal("unbounded output")
	}
	if output.data.Len() != 0 {
		t.Fatal("oversized output retained")
	}
}

func TestInheritedConservativeDefaults(t *testing.T) {
	for _, limit := range []string{"", "384MiB"} {
		env := "\n" + strings.Join(conservativeEnv([]string{"GOMEMLIMIT=" + limit}), "\n") + "\n"
		want := limit
		if want == "" {
			want = "512MiB"
		}
		if !strings.Contains(env, "\nGOMEMLIMIT="+want+"\n") {
			t.Fatalf("memory default/override not retained: %q", env)
		}
	}
	for _, tc := range []struct {
		in           []string
		flags, procs string
	}{
		{[]string{"GOFLAGS=-tags=fixture"}, "-tags=fixture -p=1", "2"},
		{[]string{"GOFLAGS=-p=3 -tags=fixture", "GOMAXPROCS=4"}, "-p=3 -tags=fixture", "4"},
		{[]string{"GOFLAGS=-p 3", "GOMAXPROCS=1"}, "-p 3", "1"},
	} {
		env := "\n" + strings.Join(conservativeEnv(tc.in), "\n") + "\n"
		if !strings.Contains(env, "\nGOFLAGS="+tc.flags+"\n") || !strings.Contains(env, "\nGOMAXPROCS="+tc.procs+"\n") {
			t.Fatalf("environment: %q", env)
		}
	}
}
