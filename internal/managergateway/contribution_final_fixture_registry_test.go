package managergateway

import (
	"fmt"
	"sync"

	"github.com/berryhill/aegis/internal/delivery"
)

// Test-only dispatch keyed by the immutable root, not the changing candidate.
// Each callback owns its HTTP fixture; the registry never holds its lock while
// invoking it (including SetPlan and any subsequent protocol work).
type finalFixtureRoot struct{ run, contract string }
type finalFixtureEntry struct {
	resolve func(delivery.Plan) (*delivery.GitHub, error)
}
type finalFixtureRegistry struct {
	mu      sync.Mutex
	entries map[finalFixtureRoot]*finalFixtureEntry
}

func finalFixtureKey(p delivery.Plan) (finalFixtureRoot, error) {
	if p.Run == "" || p.Contract == "" {
		return finalFixtureRoot{}, fmt.Errorf("missing controlled fixture root")
	}
	return finalFixtureRoot{p.Run, p.Contract}, nil
}

func (r *finalFixtureRegistry) register(p delivery.Plan, resolve func(delivery.Plan) (*delivery.GitHub, error)) (func(), error) {
	key, err := finalFixtureKey(p)
	if err != nil {
		return nil, err
	}
	if resolve == nil {
		return nil, fmt.Errorf("missing controlled fixture callback")
	}
	entry := &finalFixtureEntry{resolve: resolve}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[finalFixtureRoot]*finalFixtureEntry)
	}
	if r.entries[key] != nil {
		return nil, fmt.Errorf("duplicate controlled fixture root")
	}
	r.entries[key] = entry
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		// A delayed/repeated old cleanup cannot remove a replacement registration.
		if r.entries[key] == entry {
			delete(r.entries, key)
		}
	}, nil
}

func (r *finalFixtureRegistry) resolve(p delivery.Plan) (*delivery.GitHub, error) {
	key, err := finalFixtureKey(p)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	entry := r.entries[key]
	r.mu.Unlock()
	if entry == nil {
		return nil, fmt.Errorf("unknown controlled fixture root")
	}
	return entry.resolve(p)
}
