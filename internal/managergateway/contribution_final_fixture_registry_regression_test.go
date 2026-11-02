package managergateway

import (
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/delivery"
)

func TestFinalFixtureRegistryIsolation(t *testing.T) {
	var r finalFixtureRegistry
	a := delivery.Plan{Reference: delivery.Reference{Run: "run-a", Contract: "contract", Candidate: "initial"}}
	b := a
	b.Run = "run-b"
	c := a
	c.Contract = "other-contract"
	var cleanups []func()
	for _, p := range []delivery.Plan{a, b, c} {
		remote := &delivery.GitHub{}
		key := p.Reference
		cleanup, err := r.register(p, func(got delivery.Plan) (*delivery.GitHub, error) {
			if got.Run != key.Run || got.Contract != key.Contract {
				t.Error("cross-root callback")
			}
			// Reentrant lookup/registration must not deadlock under the dispatcher lock.
			missing := got
			missing.Run = "unknown"
			if _, err := r.resolve(missing); err == nil {
				t.Error("unknown root accepted")
			}
			return remote, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		cleanups = append(cleanups, cleanup)
		t.Cleanup(cleanup)
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				successor := p
				successor.Candidate = "successor"
				got, err := r.resolve(successor)
				if err != nil || got != remote {
					t.Errorf("wrong fixture for successor: %p %v", got, err)
				}
			}()
		}
		wg.Wait()
	}
	if _, err := r.register(a, func(delivery.Plan) (*delivery.GitHub, error) { t.Error("duplicate callback invoked"); return nil, nil }); err == nil {
		t.Fatal("duplicate accepted")
	}
	cleanups[0]()
	if _, err := r.resolve(a); err == nil {
		t.Fatal("cleaned root accepted")
	}
	if _, err := r.resolve(b); err != nil {
		t.Fatal("cleanup removed other root", err)
	}
	replacement := &delivery.GitHub{}
	cleanup, err := r.register(a, func(delivery.Plan) (*delivery.GitHub, error) { return replacement, nil })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	cleanups[0]()
	if got, err := r.resolve(a); err != nil || got != replacement {
		t.Fatal("stale cleanup removed replacement", err)
	}
	for _, p := range []delivery.Plan{{}, {Reference: delivery.Reference{Run: "run-a"}}, {Reference: delivery.Reference{Contract: "contract"}}, {Reference: delivery.Reference{Run: "run-a ", Contract: "contract"}}} {
		if _, err := r.resolve(p); err == nil {
			t.Error("nonexact/incomplete root accepted")
		}
	}
	if _, err := r.register(delivery.Plan{}, func(delivery.Plan) (*delivery.GitHub, error) { return nil, nil }); err == nil {
		t.Error("empty registration accepted")
	}
	if _, err := r.register(a, nil); err == nil {
		t.Error("nil callback accepted")
	}
}

func TestFinalFixtureRegistryConcurrentLifecycle(t *testing.T) {
	var r finalFixtureRegistry
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := delivery.Plan{Reference: delivery.Reference{Run: t.Name() + string(rune('A'+i)), Contract: "contract"}}
			remote := &delivery.GitHub{}
			cleanup, err := r.register(p, func(delivery.Plan) (*delivery.GitHub, error) { return remote, nil })
			if err != nil {
				t.Error(err)
				return
			}
			for j := 0; j < 20; j++ {
				if got, err := r.resolve(p); err != nil || got != remote {
					t.Error("concurrent dispatch mismatch", err)
				}
			}
			var cleaners sync.WaitGroup
			for j := 0; j < 4; j++ {
				cleaners.Add(1)
				go func() { defer cleaners.Done(); cleanup() }()
			}
			cleaners.Wait()
			if _, err := r.resolve(p); err == nil {
				t.Error("cleanup failed")
			}
		}(i)
	}
	wg.Wait()
}
