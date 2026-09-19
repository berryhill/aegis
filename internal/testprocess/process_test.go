//go:build unix

package testprocess

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

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
