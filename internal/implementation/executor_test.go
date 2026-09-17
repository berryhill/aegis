package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
	"github.com/dgraph-io/badger/v4"
)

type proposerFunc func(context.Context, Request) ([]Edit, error)

func (f proposerFunc) Propose(c context.Context, r Request) ([]Edit, error) { return f(c, r) }

func fixture(t *testing.T) (*Executor, loop.VerifiedImplementation) {
	t.Helper()
	root := t.TempDir()
	for p, b := range map[string]string{"go.mod": "module synthetic\n\ngo 1.24\n", "value.go": "package synthetic\nfunc Value() int { return 0 }\n", "value_test.go": "package synthetic\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value()!=42 { t.Fatal(\"acceptance not satisfied\") } }\n"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := badger.Open(badger.DefaultOptions(t.TempDir()).WithLogger(nil).WithSyncWrites(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	goBin, err = filepath.Abs(goBin)
	if err != nil {
		t.Fatal(err)
	}
	c := loop.ImplementationDraft("Return 42", "TestValue passes")
	c.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "synthetic", Name: "TestValue"}}
	c.Workspace = root
	c.WritableFiles = []string{"value.go"}
	c.Policy.TimeoutSeconds = 120
	return &Executor{DB: db, GoBinary: goBin, Admit: func(context.Context, string) error { return nil }}, c
}
func edits(value string) []Edit {
	return []Edit{{Path: "value.go", Content: []byte("package synthetic\nfunc Value() int { return " + value + " }\n")}}
}

func TestRealGoVerification(t *testing.T) {
	for _, tt := range []struct {
		name      string
		correctAt int
		want      string
		passes    int
	}{{"first-pass", 1, "succeeded", 1}, {"correction", 2, "succeeded", 2}, {"exhaustion", 3, "failed", 2}} {
		t.Run(tt.name, func(t *testing.T) {
			e, c := fixture(t)
			calls := 0
			e.Proposer = proposerFunc(func(_ context.Context, r Request) ([]Edit, error) {
				calls++
				if calls > 1 && len(r.PreviousCheck) == 0 {
					t.Fatal("correction lacks controller feedback")
				}
				if calls >= tt.correctAt {
					return edits("42"), nil
				}
				return edits("1"), nil
			})
			r, err := e.Run(context.Background(), "run-1", c)
			if r.State != tt.want || len(r.Passes) != tt.passes {
				t.Fatalf("record=%+v err=%v", r, err)
			}
			if (err == nil) != (tt.want == "succeeded") {
				t.Fatalf("unexpected error: %v", err)
			}
			read, err := e.Read("run-1")
			if err != nil || read.State != tt.want {
				t.Fatalf("durable readback: %+v %v", read, err)
			}
			if _, err = e.Run(context.Background(), "run-1", c); err == nil || calls != tt.passes {
				t.Fatal("replay replenished budget")
			}
			if tt.want == "succeeded" {
				if err = e.Revalidate("run-1", c); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(c.Workspace, "value.go"), []byte("tampered"), 0600); err != nil {
					t.Fatal(err)
				}
				if err = e.Revalidate("run-1", c); err == nil {
					t.Fatal("tampered workspace accepted")
				}
			}
		})
	}
}

func TestAuthorityAndModelBoundary(t *testing.T) {
	for _, state := range []string{"denied", "expired", "revoked", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			e, c := fixture(t)
			e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) { return edits("42"), nil })
			e.Admit = func(_ context.Context, effect string) error {
				if strings.HasPrefix(effect, "write:") {
					return &Halt{State: state}
				}
				return nil
			}
			r, err := e.Run(context.Background(), "denied-run", c)
			if err == nil || r.State != state {
				t.Fatalf("%+v %v", r, err)
			}
			b, _ := os.ReadFile(filepath.Join(c.Workspace, "value.go"))
			if !strings.Contains(string(b), "return 0") {
				t.Fatal("denied write happened")
			}
		})
	}
	e, c := fixture(t)
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) {
		return []Edit{{Path: "value_test.go", Content: []byte("PASS")}}, nil
	})
	if _, err := e.Run(context.Background(), "forged-test", c); err == nil {
		t.Fatal("model rewrote acceptance test")
	}
}

func TestContractStrictAndUnresolved(t *testing.T) {
	c := loop.ImplementationDraft("task", "acceptance")
	if c.Validate() == nil {
		t.Fatal("unresolved draft executable")
	}
	c.Policy.RequiredTests = []loop.RequiredGoTest{{Package: "synthetic", Name: "TestValue"}}
	c.Workspace = t.TempDir()
	c.WritableFiles = []string{"value.go"}
	b, _ := json.Marshal(c)
	if _, err := loop.DecodeVerifiedImplementation(b); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(string(b), `"kind":"go-test.v1"`, `"kind":"shell","command":"touch /tmp/unsafe"`, 1), strings.Replace(string(b), `"max_passes":2`, `"max_passes":3`, 1), strings.Replace(string(b), `"task":"task"`, `"task":"task","task":"replacement"`, 1)} {
		if _, err := loop.DecodeVerifiedImplementation([]byte(bad)); err == nil {
			t.Fatalf("unsafe contract accepted: %s", bad)
		}
	}
}

func TestInterruptedReservationAndEvidenceTamper(t *testing.T) {
	e, c := fixture(t)
	cd, _ := c.Digest()
	r := Record{RunID: "interrupted", ContractDigest: cd, State: "running", Passes: []Pass{{ID: "interrupted/pass/1"}}}
	if err := e.save(r); err != nil {
		t.Fatal(err)
	}
	e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) {
		t.Fatal("interrupted run replayed")
		return nil, errors.New("unexpected")
	})
	if _, err := e.Run(context.Background(), r.RunID, c); err == nil {
		t.Fatal("interrupted budget reset")
	}
	d, err := e.blob([]byte("actual controller output"))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.DB.Update(func(tx *badger.Txn) error { return tx.Set(blobKey(d), []byte("PASS")) }); err != nil {
		t.Fatal(err)
	}
	if _, err = e.loadBlob(d); err == nil {
		t.Fatal("content-addressed evidence tamper accepted")
	}
}
