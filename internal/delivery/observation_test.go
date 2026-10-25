package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestV7LoopbackPartialMergeThenAdvancedBaseGreen(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.lost, f.postFail = true, true
	m := &memoryOwner{}
	e := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, g)
	ctx := context.Background()
	first, err := e.Execute(ctx, p.Reference, Merge)
	if err != nil || first.Outcome != Unknown || first.Merge != "" {
		t.Fatal(first, err)
	}
	partial, err := e.Execute(ctx, p.Reference, Merge)
	if err != nil || partial.Merge != strings.Repeat("e", 40) || partial.Tree != p.Commit.Tree || partial.Head != p.Head || partial.PR != p.PR || partial.Code != "merged_validation_unresolved" {
		t.Fatal(partial, err)
	}
	repeated, err := e.Execute(ctx, p.Reference, Merge)
	if err != nil || repeated.Outcome != Unknown || repeated.Merge != partial.Merge {
		t.Fatal(repeated, err)
	}
	f.Lock()
	f.base = strings.Repeat("f", 40)
	f.Unlock()
	i, _ := intentFor(p, Merge)
	current := g.Reconcile(ctx, i, p, func(context.Context) error { return nil })
	if current.BaseHead != strings.Repeat("f", 40) || current.Merge != partial.Merge || current.Outcome != Unknown {
		t.Fatal(current)
	}
	f.Lock()
	f.postFail = false
	f.Unlock()
	current = g.Reconcile(ctx, i, p, func(context.Context) error { return nil })
	if current.Outcome != Confirmed || current.BaseHead != strings.Repeat("f", 40) {
		t.Fatal(current)
	}
	done, err := e.Execute(ctx, p.Reference, Merge)
	if err != nil || done.Outcome != Confirmed || done.Intent != first.Intent || done.Merge != partial.Merge || done.Tree != partial.Tree || done.Head != partial.Head || done.PR != partial.PR || done.BaseHead != partial.BaseHead {
		t.Fatal(done, err)
	}
	f.Lock()
	writes := f.writes
	f.Unlock()
	if writes != 1 {
		t.Fatal(writes)
	}
}

type v7Adapter struct {
	entered, release chan struct{}
	read             Observation
	writes           int
}

func (a *v7Adapter) Apply(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	a.writes++
	close(a.entered)
	<-a.release
	return Observation{Outcome: Unknown, Code: "response_lost"}
}
func (a *v7Adapter) Reconcile(ctx context.Context, i Intent, p Plan, g Guard) Observation {
	if g(ctx) != nil {
		return Observation{Outcome: Denied}
	}
	return a.read
}
func TestV7ConcurrentReadAndOriginalFinish(t *testing.T) {
	for _, positive := range []bool{false, true} {
		t.Run(map[bool]string{false: "negative", true: "partial"}[positive], func(t *testing.T) {
			p := fixturePlan()
			p.PR = 7
			p.Commit.Tree = strings.Repeat("d", 40)
			m := &memoryOwner{}
			a := &v7Adapter{entered: make(chan struct{}), release: make(chan struct{}), read: Observation{Outcome: Failed, Code: "not_seen"}}
			if positive {
				a.read = Observation{Outcome: Unknown, Merge: strings.Repeat("e", 40), Head: p.Head, Tree: p.Commit.Tree, PR: p.PR, Code: "merged_validation_unresolved"}
			}
			e := NewEngine(m, fixtureSource{p}, &fixtureAuthority{}, a)
			type answer struct {
				r Record
				e error
			}
			done := make(chan answer, 1)
			go func() { r, x := e.Execute(context.Background(), p.Reference, Merge); done <- answer{r, x} }()
			<-a.entered
			read, x := e.Execute(context.Background(), p.Reference, Merge)
			if x != nil || read.Outcome != Unknown {
				t.Fatal(read, x)
			}
			if !positive && read.Revision != 1 {
				t.Fatal("negative consumed CAS", read)
			}
			close(a.release)
			original := <-done
			if original.e != nil || original.r.Outcome != Unknown || original.r.Merge != a.read.Merge || original.r.Intent != read.Intent || a.writes != 1 {
				t.Fatal(original, a.writes)
			}
			if positive && original.r.Code != "merged_validation_unresolved" {
				t.Fatal(original)
			}
		})
	}
}

func TestV7ObservationValidationAndAbsentFacts(t *testing.T) {
	p := fixturePlan()
	p.PR = 7
	p.Commit.Tree = strings.Repeat("d", 40)
	i, _ := intentFor(p, Merge)
	for _, o := range []Observation{{Head: strings.Repeat("f", 40)}, {Merge: "bad"}, {Tree: strings.Repeat("f", 40)}, {PR: 8}} {
		if validateObservation(i, p, o) == nil {
			t.Fatal("accepted unrelated fact", o)
		}
	}
	old := Record{Observation: Observation{Merge: strings.Repeat("e", 40), Head: p.Head, Tree: p.Commit.Tree, PR: 7, BaseHead: strings.Repeat("e", 40)}}
	joined, err := retainObservation(old, Observation{Outcome: Failed})
	if err != nil || joined.Merge != old.Merge || joined.Head != old.Head || joined.Tree != old.Tree || joined.PR != 7 || joined.Outcome != Unknown {
		t.Fatal(joined, err)
	}
	if _, err = retainObservation(old, Observation{Merge: strings.Repeat("f", 40)}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestV7StoppedReadNeedsAuthority(t *testing.T) {
	g, f, p := newGithubFixture(t)
	f.lost, f.postFail = true, true
	m := &memoryOwner{}
	auth := &fixtureAuthority{}
	e := NewEngine(m, fixtureSource{p}, auth, g)
	if _, err := e.Execute(context.Background(), p.Reference, Merge); err != nil {
		t.Fatal(err)
	}
	m.cancelled = true // write fence revoked, separately valid read authority retained
	r, err := e.Execute(context.Background(), p.Reference, Merge)
	if err != nil || r.Merge == "" || r.Outcome != Unknown {
		t.Fatal(r, err)
	}
	auth.denied = true
	if _, err = e.Execute(context.Background(), p.Reference, Merge); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = e.Execute(ctx, p.Reference, Merge); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	f.Lock()
	writes := f.writes
	f.Unlock()
	if writes != 1 {
		t.Fatal(writes)
	}
}
