package implementation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

const addWrong = "package synthetic\nfunc Add(a, b int) int { return a-b }\n"
const addRight = "package synthetic\nfunc Add(a, b int) int { return a+b }\n"
const addTests = `package synthetic
import "testing"
func TestAdd(t *testing.T) {
 for _, tc := range []struct{a,b,want int}{{2,3,5},{-2,3,1},{0,0,0}} {
  if got:=Add(tc.a,tc.b); got!=tc.want { t.Fatal("HOST_OUTPUT_SENTINEL",got,tc.want) }
 }
}
`

func addContextFixture(t *testing.T) (*Executor, loop.VerifiedImplementation) {
	t.Helper()
	e, c := fixture(t)
	// Rename the fixture's two files, preserving a dependency-free real Go module.
	for old, name := range map[string]string{"value.go": "add.go", "value_test.go": "add_test.go"} {
		if err := os.Rename(filepath.Join(c.Workspace, old), filepath.Join(c.Workspace, name)); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range map[string]string{"add.go": addWrong, "add_test.go": addTests} {
		if err := os.WriteFile(filepath.Join(c.Workspace, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	c.Task = "Repair Add"
	c.Acceptance = "Immutable TestAdd table passes"
	c.WritableFiles = []string{"add.go"}
	c.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "synthetic", Name: "TestAdd"}}
	c.RepositoryContext = &loop.RepositoryContext{TaskDigest: digest([]byte(c.Task)), MaxFiles: 3, MaxFileBytes: 4096, MaxTotalBytes: 8192}
	for _, f := range []struct{ path, role, text string }{{"add.go", "source", addWrong}, {"add_test.go", "trusted_test", addTests}, {"go.mod", "checker_input", "module synthetic\n\ngo 1.24\n"}} {
		c.RepositoryContext.Files = append(c.RepositoryContext.Files, loop.RepositoryContextFile{Path: f.path, Role: f.role, Bytes: int64(len(f.text)), Digest: digest([]byte(f.text))})
	}
	c.RepositoryContext.SourceDigest = RepositorySourceDigest(c.RepositoryContext)
	return e, c
}

func assertClosedFeedback(t *testing.T, b []byte) {
	t.Helper()
	if len(b) == 0 || len(b) > 65536 || bytes.Contains(b, []byte("HOST_OUTPUT_SENTINEL")) || bytes.Contains(b, []byte("Output")) || !bytes.Contains(b, []byte("go_test_failed")) {
		t.Fatalf("not closed feedback: %s", b)
	}
}

// Synthetic deterministic proposer; actual offline go-test.v1 execution. This
// is not managed-provider or installed product acceptance.
func TestContextExecutorRepresentative(t *testing.T) {
	e, c := addContextFixture(t)
	ctx := context.Background()
	raw, passed, err := e.check(ctx, c)
	if err != nil || passed || !bytes.Contains(raw, []byte("HOST_OUTPUT_SENTINEL")) {
		t.Fatalf("RED missing: passed=%v err=%v", passed, err)
	}
	t.Log("RED: actual go-test.v1 rejected deliberate Add subtraction defect")
	c = doerContract(c)
	c.MaxPasses = 2
	e.Decision = &decisionStub{gate: GateDecision{true, true}, verdict: Judgment{true, true, true, true, true}}
	diagnoses, calls := 0, 0
	e.Diagnosis = diagnosisFunc(func(_ context.Context, r DiagnosisRequest) ([]byte, error) {
		diagnoses++
		assertClosedFeedback(t, r.Check)
		return []byte("repair addition"), nil
	})
	e.Proposer = reportProposer(func(_ context.Context, r Request) (Proposal, error) {
		calls++
		if r.RepositoryContext == nil || r.RepositoryContext.Files[0].Text != addWrong || r.RepositoryContext.Files[1].Text != addTests {
			t.Fatal("initial source/test bytes changed or absent")
		}
		if calls > 1 {
			assertClosedFeedback(t, r.PreviousCheck)
		}
		// Neither context bytes nor manifest policy alias controller custody.
		r.RepositoryContext.Files[0].Text = "poison"
		r.Contract.RepositoryContext.Files[1].Digest = digest([]byte("poison"))
		text := strings.Replace(addWrong, "a-b", "a*b", 1)
		if calls == 2 {
			text = addRight
		}
		return Proposal{Edits: []Edit{{Path: "add.go", Content: []byte(text)}}, Report: "synthetic patch"}, nil
	})
	r, err := e.Run(ctx, "context-repair", c)
	if err != nil || r.State != "succeeded" || calls != 2 || diagnoses != 1 || len(r.Passes) != 2 {
		t.Fatalf("GREEN failed: %+v %v", r, err)
	}
	if err = e.Revalidate(r.RunID, c); err != nil {
		t.Fatal(err)
	}
	frozen, err := os.ReadFile(filepath.Join(c.Workspace, "add_test.go"))
	if err != nil || string(frozen) != addTests {
		t.Fatal("trusted bytes changed")
	}
	t.Log("GREEN: real TestAdd passed after exactly two synthetic proposals; immutable tests and bound evidence reloaded")
	if _, err = e.Run(ctx, r.RunID, c); err == nil || calls != 2 {
		t.Fatal("budget reset")
	}
	original := r
	r.ContextDigest = ""
	if err = e.save(r); err != nil {
		t.Fatal(err)
	}
	if e.Revalidate(r.RunID, c) == nil {
		t.Fatal("missing context accepted")
	}
	r = original
	r.Passes = append([]Pass(nil), original.Passes...)
	r.Passes[0].ContextIdentity = "wrong"
	if err = e.save(r); err != nil {
		t.Fatal(err)
	}
	if e.Revalidate(r.RunID, c) == nil {
		t.Fatal("mismatched pass context accepted")
	}
	if err = e.save(original); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(c.Workspace, "add_test.go"), []byte("package synthetic\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if e.Revalidate(r.RunID, c) == nil {
		t.Fatal("trusted test mutation accepted")
	}
}

func TestContextExecutorBoundaryDrift(t *testing.T) {
	for _, phase := range []string{"source_binding", "proposal_source", "proposal_test", "write_source", "check_test", "complete_source"} {
		t.Run(phase, func(t *testing.T) {
			e, c := addContextFixture(t)
			calls := 0
			if phase == "source_binding" {
				c.RepositoryContext.SourceDigest = digest([]byte("unrelated source state"))
			}
			mutate := func(name string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(c.Workspace, name), []byte("package synthetic\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			e.Proposer = proposerFunc(func(_ context.Context, r Request) ([]Edit, error) {
				calls++
				if phase == "proposal_source" {
					mutate("add.go")
				}
				if phase == "proposal_test" {
					mutate("add_test.go")
				}
				return []Edit{{Path: "add.go", Content: []byte(addRight)}}, nil
			})
			e.Admit = func(_ context.Context, effect string) error {
				if phase == "write_source" && effect == "write:add.go" {
					mutate("add.go")
				}
				if phase == "check_test" && effect == "check" {
					mutate("add_test.go")
				}
				if phase == "complete_source" && effect == "complete" {
					mutate("add.go")
				}
				return nil
			}
			r, err := e.Run(context.Background(), "drift", c)
			if err == nil || r.State == "succeeded" {
				t.Fatalf("drift accepted: %+v %v", r, err)
			}
			if phase == "source_binding" && calls != 0 {
				t.Fatal("model called before source binding")
			}
		})
	}
}

func TestContextExecutorWrongPackageAndBoundedFailure(t *testing.T) {
	e, c := addContextFixture(t)
	calls := 0
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) {
		calls++
		return []Edit{{Path: "add.go", Content: []byte("package wrong\n")}}, nil
	})
	r, err := e.Run(context.Background(), "wrong-package", c)
	if err == nil || r.State != "failed" || calls != 2 || len(r.Passes) != 2 {
		t.Fatalf("stub accepted or unbounded: %+v %v", r, err)
	}
	if e.Revalidate(r.RunID, c) == nil {
		t.Fatal("failed acceptance revalidated")
	}
	required := c.Policy.RequiredTests
	for _, raw := range []string{"", `{"Action":"pass","Package":"synthetic"}`, `{"Action":"run","Package":"synthetic","Test":"TestAdd"}` + "\n" + `{"Action":"fail","Package":"synthetic","Test":"TestAdd","Output":"HOST_OUTPUT_SENTINEL"}`} {
		if requiredTestsPassed([]byte(raw), required) {
			t.Fatal("missing/failed required acceptance passed")
		}
		assertClosedFeedback(t, closedCheckDiagnostics([]byte(raw), false, required))
	}
}

func TestContextExecutorNilCompatibility(t *testing.T) {
	c := loop.ImplementationDraft("task", "acceptance")
	b, err := json.Marshal(Request{Contract: c})
	if err != nil || bytes.Contains(b, []byte("RepositoryContext")) {
		t.Fatalf("nil request changed: %s %v", b, err)
	}
	m := &loop.RepositoryContext{Files: []loop.RepositoryContextFile{{Path: "add.go", Role: "source", Bytes: 1, Digest: digest([]byte("a"))}}}
	d := RepositorySourceDigest(m)
	m.SourceDigest = "different"
	if RepositorySourceDigest(m) != d {
		t.Fatal("self-referential source binding")
	}
	m.Files[0].Digest = digest([]byte("b"))
	if RepositorySourceDigest(m) == d {
		t.Fatal("source preimage not bound")
	}
}
