package implementation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Actual Badger custody and offline Go checker; deterministic proposal fixture.
func TestNativeContinuation(t *testing.T) {
	t.Parallel()
	for _, correction := range []bool{true, false} {
		t.Run(map[bool]string{true: "corrected", false: "failed_acceptance"}[correction], func(t *testing.T) {
			e, c := addContextFixture(t)
			c.MaxPasses = 2
			e.PassLimit = 1
			e.Proposer = proposerFunc(func(context.Context, Request) ([]Edit, error) {
				return []Edit{{Path: "add.go", Content: []byte("package synthetic\nfunc Add(a,b int) int { return a*b }\n")}}, nil
			})
			first, err := e.Run(context.Background(), "first", c)
			if err == nil || first.State != "failed" || len(first.Passes) != 1 {
				t.Fatalf("first %+v %v", first, err)
			}
			history, err := e.History("first", c)
			if err != nil {
				t.Fatal(err)
			}
			if history.Recovery.Effect != "apply_observed" || history.Recovery.Decision != "linked_requeue_requires_fresh_checks" {
				t.Fatal(history.Recovery)
			}
			for _, bad := range []struct {
				head, id string
				cap      uint16
			}{{"wrong", "next", 1}, {history.Digest, "first", 1}, {history.Digest, "next", 2}} {
				if _, err = e.PrepareContinuation("first", bad.head, bad.id, bad.cap, c); err == nil {
					t.Fatal("bad lineage accepted")
				}
			}
			changed := c
			changed.Task = "swapped"
			if _, err = e.PrepareContinuation("first", history.Digest, "next", 1, changed); err == nil {
				t.Fatal("changed task accepted")
			}
			current, err := os.ReadFile(filepath.Join(c.Workspace, "add.go"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(c.Workspace, "add.go"), []byte(addRight), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = e.PrepareContinuation("first", history.Digest, "next", 1, c); err == nil {
				t.Fatal("drift accepted")
			}
			if err = os.WriteFile(filepath.Join(c.Workspace, "add.go"), current, 0600); err != nil {
				t.Fatal(err)
			}
			e.Continuation, err = e.PrepareContinuation("first", history.Digest, "next", 1, c)
			if err != nil {
				t.Fatal(err)
			}
			e.PassLimit = 0
			calls := 0
			e.Proposer = proposerFunc(func(_ context.Context, r Request) ([]Edit, error) {
				calls++
				if r.RepositoryContext.Files[0].Text != addWrong {
					t.Fatal("original provenance lost")
				}
				text := string(current)
				if correction {
					text = addRight
				}
				return []Edit{{Path: "add.go", Content: []byte(text)}}, nil
			})
			next, err := e.Run(context.Background(), "next", c)
			if calls != 1 || next.ConsumedBefore != 1 || len(next.Passes) != 1 {
				t.Fatalf("budget %+v calls %d %v", next, calls, err)
			}
			if correction {
				if err != nil || next.State != "succeeded" {
					t.Fatalf("correction %+v %v", next, err)
				}
				if _, err = e.Output("next", c); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || next.State != "failed" {
				t.Fatal("failed acceptance succeeded")
			}
			after, err := e.History("first", c)
			if err != nil || after.Digest != history.Digest {
				t.Fatal("predecessor changed")
			}
			nh, err := e.History("next", c)
			if err != nil {
				t.Fatal(err)
			}
			if history.ConsumedBefore != 0 || history.CumulativeConsumed != 1 || history.RemainingBudget != 1 || nh.ConsumedBefore != 1 || nh.CumulativeConsumed != 2 || nh.RemainingBudget != 0 || nh.PredecessorRun != "first" || nh.PredecessorHistory != history.Digest || nh.ReservedPasses != 1 {
				t.Fatalf("cumulative custody projection: first=%+v next=%+v", history, nh)
			}
			if _, err = e.PrepareContinuation("next", nh.Digest, "third", 1, c); err == nil {
				t.Fatal("exhausted or success replay accepted")
			}
			if _, err = e.Run(context.Background(), "next", c); err == nil {
				t.Fatal("successor replay accepted")
			}
			if !correction {
				e.Continuation, err = e.PrepareContinuation("first", history.Digest, "duplicate", 1, c)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = e.Run(context.Background(), "duplicate", c); err == nil {
					t.Fatal("duplicate successor accepted")
				}
			}
		})
	}
}
