//go:build unix

package testprocess

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

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
