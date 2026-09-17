package implementation

import (
	"context"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitialAdmissionDurable(t *testing.T) {
	for _, state := range []string{"denied", "revoked", "expired", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			e, c := fixture(t)
			e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { t.Fatal("proposal after denial"); return nil, nil })
			e.Admit = func(context.Context, string) error { return &Halt{State: state} }
			r, err := e.Run(context.Background(), "reject", c)
			if err == nil || r.State != state {
				t.Fatalf("non-durable rejection: %+v %v", r, err)
			}
			saved, err := e.Read("reject")
			if err != nil || saved.State != state {
				t.Fatalf("read: %+v %v", saved, err)
			}
		})
	}
}
func TestHardlinkAliasesRejected(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "test", true: "external"}[external], func(t *testing.T) {
			e, c := fixture(t)
			target := filepath.Join(c.Workspace, "value_test.go")
			if external {
				target = filepath.Join(t.TempDir(), "outside")
				if err := os.WriteFile(target, []byte("protected"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(target)
			os.Remove(filepath.Join(c.Workspace, "value.go"))
			if err := os.Link(target, filepath.Join(c.Workspace, "value.go")); err != nil {
				t.Fatal(err)
			}
			if err := e.apply(context.Background(), c, edits("42")); err == nil {
				t.Error("hardlink accepted")
			}
			after, _ := os.ReadFile(target)
			if string(before) != string(after) {
				t.Fatal("alias overwritten")
			}
		})
	}
}
func TestInterruptedTerminalRecovery(t *testing.T) {
	e, c := fixture(t)
	cd, _ := c.Digest()
	e.save(Record{RunID: "crash", ContractDigest: cd, State: "running", Passes: []Pass{{ID: "crash/pass/1"}}})
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { t.Fatal("replayed"); return nil, nil })
	e.Run(context.Background(), "crash", c)
	r, err := e.Read("crash")
	if err != nil || r.State != "interrupted" || len(r.Passes) != 1 {
		t.Fatalf("not terminal: %+v %v", r, err)
	}
}
func TestNoTestsCannotPass(t *testing.T) {
	e, c := fixture(t)
	os.Remove(filepath.Join(c.Workspace, "value_test.go"))
	_, passed, err := e.check(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if passed {
		t.Fatal("no tests passed verification")
	}
}
func TestRequiredTestEvidence(t *testing.T) {
	required := []loop.RequiredGoTest{{Package: "synthetic", Name: "TestValue"}}
	run := `{"Action":"run","Package":"synthetic","Test":"TestValue"}` + "\n"
	pass := `{"Action":"pass","Package":"synthetic","Test":"TestValue"}` + "\n"
	for _, bad := range []string{"", pass, run, run + `{"Action":"skip","Package":"synthetic","Test":"TestValue"}`, strings.ReplaceAll(run+pass, "TestValue", "TestOther"), run + pass + `{"Action":"fail","Package":"synthetic"}`} {
		if requiredTestsPassed([]byte(bad), required) {
			t.Fatalf("bad evidence accepted: %s", bad)
		}
	}
	if !requiredTestsPassed([]byte(run+pass), required) {
		t.Fatal("valid evidence rejected")
	}
}
func TestSkippedRequiredTestFails(t *testing.T) {
	e, c := fixture(t)
	os.WriteFile(filepath.Join(c.Workspace, "value_test.go"), []byte("package synthetic\nimport \"testing\"\nfunc TestValue(t *testing.T){t.Skip(\"not run\")}\n"), 0600)
	_, passed, err := e.check(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if passed {
		t.Fatal("skipped acceptance passed")
	}
}
func TestReservedRejectionNeverReplaced(t *testing.T) {
	e, c := fixture(t)
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { t.Fatal("proposal"); return nil, nil })
	e.Admit = func(context.Context, string) error { return &Halt{State: "denied"} }
	first, _ := e.Run(context.Background(), "reserved", c)
	e.Admit = func(context.Context, string) error { return &Halt{State: "revoked"} }
	e.Run(context.Background(), "reserved", c)
	last, _ := e.Read("reserved")
	if last.State != first.State || last.ContractDigest != first.ContractDigest {
		t.Fatal("reserved outcome replaced")
	}
}
