//go:build linux

package hermes

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/execution"
	"golang.org/x/sys/unix"
)

func runLateForkBarrierFixture(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `(while [ ! -f "$1/go" ]; do :; done; (while :; do printf x >> "$1/writes"; sleep 0.001; done) & echo $! > "$1/writer"; exit 0) & echo $! > "$1/worker"; exit 0`
	cmd := exec.Command("/bin/sh", "-c", script, "fixture", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transitioned, joined, initialEmpty := false, false, false
	writer, writerFD, scans := 0, -1, 0
	defer func() {
		if writerFD >= 0 {
			_ = unix.PidfdSendSignal(writerFD, unix.SIGKILL, nil, 0)
			_ = unix.Close(writerFD)
		}
	}()
	readPID := func(name string) (int, error) {
		for {
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err == nil {
				if pid, e := strconv.Atoi(strings.TrimSpace(string(raw))); e == nil && pid > 1 {
					return pid, nil
				}
			}
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			time.Sleep(time.Millisecond)
		}
	}
	unsafe := os.Getenv("AEGIS_TEST_UNSAFE_STALE_INVENTORY") == "1"
	err := runContributionNativeWithBarrier(ctx, execution.NativeBinding{CommandDigest: NativeCommandDigest(cmd)}, cmd,
		func(context.Context) error { return nil },
		func(_ context.Context, p NativeCompletion) error { joined = p.Joined(); return nil },
		func(ctx context.Context, group int) (bool, error) {
			scans++
			if unsafe {
				return initialEmpty, nil
			} // Deliberately unsafe old inventory-only algorithm.
			return terminateContributionGroup(ctx, group)
		},
		func(cleanup context.Context, group int) error {
			worker, e := readPID("worker")
			if e != nil {
				return e
			}
			entries, e := os.ReadDir("/proc")
			if e != nil {
				return e
			}
			found := false
			for _, entry := range entries {
				if entry.Name() == strconv.Itoa(worker) {
					found = true
				}
			}
			if !found {
				t.Fatal("enumerated parent missing before release")
			}
			if e = os.WriteFile(filepath.Join(dir, "go"), nil, 0600); e != nil {
				return e
			}
			writer, e = readPID("writer")
			if e != nil {
				return e
			}
			for _, entry := range entries {
				if entry.Name() == strconv.Itoa(writer) {
					t.Fatal("writer was not newly published after enumeration")
				}
			}
			writerFD, e = unix.PidfdOpen(writer, 0)
			if e != nil {
				return e
			}
			for {
				_, pg, ws, we := contributionProcessStatFrom(writer, os.ReadFile)
				_, _, state, se := contributionProcessStatFrom(worker, os.ReadFile)
				writes, writeErr := os.ReadFile(filepath.Join(dir, "writes"))
				if we == nil && pg == group && ws != "Z" && ws != "X" && ws != "x" && (contributionProcDisappeared(se) || state == "Z") && writeErr == nil && len(writes) > 0 {
					transitioned = true
					break
				}
				if cleanup.Err() != nil {
					return cleanup.Err()
				}
				time.Sleep(time.Millisecond)
			}
			initialEmpty, e = terminateContributionGroupFrom(cleanup, group, func(string) ([]os.DirEntry, error) { return entries, nil }, os.ReadFile, unix.PidfdOpen, unix.PidfdSendSignal, unix.Close)
			if e != nil {
				return e
			}
			if !initialEmpty {
				t.Fatal("stale captured inventory did not exhibit empty-observation hole")
			}
			if unsafe {
				return nil
			}
			return killPinnedContributionGroup(cleanup, group, unix.Waitid, unix.Kill)
		})
	// This is a hard behavioral assertion; the negative child must fail HERE.
	_, _, state, stateErr := contributionProcessStatFrom(writer, os.ReadFile)
	if stateErr == nil && state != "Z" && state != "X" && state != "x" {
		t.Fatal("live writer survived acknowledged cleanup")
	}
	if stateErr != nil && !contributionProcDisappeared(stateErr) {
		t.Fatal(stateErr)
	}
	if err != nil || !transitioned || !joined || !initialEmpty {
		t.Fatalf("err=%v transition=%v joined=%v initialEmpty=%v", err, transitioned, joined, initialEmpty)
	}
	if scans == 0 {
		t.Fatal("no fresh post-barrier observer")
	}
	before, _ := os.ReadFile(filepath.Join(dir, "writes"))
	time.Sleep(30 * time.Millisecond)
	after, _ := os.ReadFile(filepath.Join(dir, "writes"))
	if len(before) != len(after) {
		t.Fatal("writer survived acknowledged cleanup")
	}
}

func TestContributionNativeUnsafeInventoryNegativeControl(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestContributionNativeForkAfterEnumeration$", "-test.count=1", "-test.timeout=8s")
	cmd.Env = append(os.Environ(), "AEGIS_TEST_UNSAFE_STALE_INVENTORY=1")
	output, err := cmd.CombinedOutput()
	t.Logf("unsafe inventory-only child (expected hard failure):\n%s", output)
	if ctx.Err() != nil || err == nil || !strings.Contains(string(output), "live writer survived acknowledged cleanup") {
		t.Fatalf("missing causal live-writer failure: err=%v output=%s", err, output)
	}
}

func TestContributionNativeBarrierNilDeniedBeforeReserve(t *testing.T) {
	cmd := exec.Command("/bin/true")
	reserved := false
	err := runContributionNativeWithBarrier(context.Background(), execution.NativeBinding{CommandDigest: NativeCommandDigest(cmd)}, cmd, func(context.Context) error { reserved = true; return nil }, func(context.Context, NativeCompletion) error { return nil }, terminateContributionGroup, nil)
	if !errors.Is(err, execution.ErrInvalidNativeCommand) || reserved || cmd.Process != nil {
		t.Fatalf("err=%v reserved=%v process=%v", err, reserved, cmd.Process)
	}
}

// Production has no environment/model/config selector and no nil/no-op default.
// Pin the public adapter's entire call shape, rather than trusting injected proof.
func TestContributionNativeProductionBarrierCannotSelectNoop(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "contribution_native_linux.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	funcs := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok {
			funcs[f.Name.Name] = f
		}
	}
	call := func(name string) *ast.CallExpr {
		t.Helper()
		f := funcs[name]
		if f == nil || len(f.Body.List) != 1 {
			t.Fatalf("%s gained a selector or fallback", name)
		}
		ret, ok := f.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			t.Fatal("not one unconditional return")
		}
		c, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			t.Fatal("not a fixed call")
		}
		return c
	}
	ident := func(e ast.Expr, want string) {
		t.Helper()
		i, ok := e.(*ast.Ident)
		if !ok || i.Name != want {
			t.Fatalf("not fixed %s", want)
		}
	}
	public := call("RunContributionNative")
	ident(public.Fun, "runContributionNative")
	if len(public.Args) != 6 {
		t.Fatal("public callback surface changed")
	}
	ident(public.Args[5], "terminateContributionGroup")
	adapter := call("runContributionNative")
	ident(adapter.Fun, "runContributionNativeWithBarrier")
	if len(adapter.Args) != 7 {
		t.Fatal("barrier adapter surface changed")
	}
	real, ok := adapter.Args[6].(*ast.FuncLit)
	if !ok || len(real.Body.List) != 1 {
		t.Fatal("not a fixed real barrier")
	}
	ret, ok := real.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		t.Fatal("barrier no-op/fallback")
	}
	kernel, ok := ret.Results[0].(*ast.CallExpr)
	if !ok || len(kernel.Args) != 4 {
		t.Fatal("barrier not real helper")
	}
	ident(kernel.Fun, "killPinnedContributionGroup")
	ident(kernel.Args[0], "ctx")
	ident(kernel.Args[1], "group")
	for i, want := range []string{"Waitid", "Kill"} {
		s, ok := kernel.Args[i+2].(*ast.SelectorExpr)
		if !ok || s.Sel.Name != want {
			t.Fatal("not real kernel operation")
		}
		ident(s.X, "unix")
	}
}

func TestContributionNativeRealBarrierObservationErrors(t *testing.T) {
	for _, mode := range []string{"eacces", "malformed", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command("/bin/sh", "-c", `(while :; do printf x >> "$1/writes"; sleep 0.001; done) & echo $! > "$1/writer"; exit 0`, "fixture", dir)
			joined := true
			observed := false
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := runContributionNative(ctx, execution.NativeBinding{CommandDigest: NativeCommandDigest(cmd)}, cmd, func(context.Context) error { return nil }, func(_ context.Context, p NativeCompletion) error { joined = p.Joined(); return nil }, func(ctx context.Context, group int) (bool, error) {
				observed = true
				// Reach actual complete production post-scans under the SAME cleanup
				// context; SIGKILL is a fork barrier, not synchronous quiescence.
				for {
					empty, scanErr := terminateContributionGroup(ctx, group)
					if scanErr != nil {
						return false, scanErr
					}
					if empty {
						break
					}
					if ctx.Err() != nil {
						return false, ctx.Err()
					}
					time.Sleep(time.Millisecond)
				}
				raw, readErr := os.ReadFile(filepath.Join(dir, "writer"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				writer, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
				if parseErr != nil || writer <= 1 {
					t.Fatalf("invalid actual writer: %q", raw)
				}
				_, _, state, statErr := contributionProcessStatFrom(writer, os.ReadFile)
				if statErr == nil && state != "Z" && state != "X" && state != "x" {
					t.Fatal("real barrier left writer live before observation error")
				}
				if statErr != nil && !contributionProcDisappeared(statErr) {
					t.Fatal(statErr)
				}
				// Reach the actual complete production post-scan before adversarial failure.
				if _, err := terminateContributionGroup(ctx, group); err != nil {
					return false, err
				}
				switch mode {
				case "eacces":
					return false, unix.EACCES
				case "malformed":
					_, _, _, err := contributionProcessStatFrom(group, func(string) ([]byte, error) { return []byte("malformed"), nil })
					return false, err
				default:
					return false, execution.ErrNativeQuiescenceUnavailable
				}
			})
			if !observed || joined || !errors.Is(err, execution.ErrNativeQuiescenceUnavailable) || cmd.ProcessState == nil {
				t.Fatalf("observed=%v joined=%v err=%v state=%v", observed, joined, err, cmd.ProcessState)
			}
		})
	}
}
