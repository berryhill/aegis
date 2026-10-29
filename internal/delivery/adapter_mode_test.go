package delivery

import (
	"context"
	"testing"
)

func TestAdapterModeIsolation(t *testing.T) {
	i := Intent{Operation: Merge}
	if CheckHistoricalReadMode(context.Background(), i) == nil {
		t.Fatal("bare context")
	}
	read, m := enterAdapterMode(context.Background(), i, true)
	if CheckHistoricalReadMode(read, i) != nil {
		t.Fatal("read positive")
	}
	other := i
	other.Operation = Publish
	if CheckHistoricalReadMode(read, other) == nil {
		t.Fatal("intent substitution")
	}
	for _, method := range []string{"POST", "PATCH", "PUT", "DELETE"} {
		if checkAdapterHTTP(read, method) == nil {
			t.Fatal(method)
		}
	}
	g := &GitHub{}
	if o := g.Apply(read, i, Plan{}, nil); o.Outcome != Denied {
		t.Fatal("nested Apply")
	}
	write, w := enterAdapterMode(read, i, false)
	if CheckHistoricalReadMode(write, i) == nil || checkAdapterHTTP(write, "PUT") == nil {
		t.Fatal("inherited read escaped")
	}
	w.active.Store(false)
	write, w = enterAdapterMode(context.Background(), i, false)
	nested, n := enterAdapterMode(write, i, true)
	if CheckHistoricalReadMode(nested, i) == nil {
		t.Fatal("write laundered through read")
	}
	n.active.Store(false)
	w.active.Store(false)
	canceled, cancel := context.WithCancel(read)
	cancel()
	if CheckHistoricalReadMode(canceled, i) == nil {
		t.Fatal("cancellation")
	}
	m.active.Store(false)
	if CheckHistoricalReadMode(read, i) == nil || checkAdapterHTTP(read, "GET") == nil {
		t.Fatal("retained mode")
	}
	if o := g.Apply(read, i, Plan{}, nil); o.Outcome != Denied {
		t.Fatal("retained Apply")
	}
}
