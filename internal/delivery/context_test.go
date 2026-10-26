package delivery

import (
	"context"
	"testing"
)

// Fake owner transport qualification only: this does not mint #377 private proof.
type sentinelSource struct {
	p         Plan
	ctx       context.Context
	saved     func(context.Context, Plan) error
	calls     int
	duplicate bool
}

func (s *sentinelSource) WithAccepted(_ context.Context, _ Reference, f func(context.Context, Plan) error) error {
	s.calls++
	s.saved = f
	err := f(s.ctx, s.p)
	if s.duplicate && f(s.ctx, s.p) == nil {
		panic("duplicate callback authorized")
	}
	return err
}

type sentinelPorts struct {
	t     *testing.T
	ctx   context.Context
	m     memoryOwner
	calls map[string]int
	guard Guard
	deny  bool
}

func (s *sentinelPorts) check(c context.Context, n string) {
	s.t.Helper()
	if c != s.ctx {
		s.t.Fatalf("%s replaced effect context", n)
	}
	s.calls[n]++
}
func (s *sentinelPorts) Reserve(c context.Context, i Intent) (Record, bool, error) {
	s.check(c, "reserve")
	return s.m.Reserve(c, i)
}
func (s *sentinelPorts) Finish(c context.Context, r Record, o Observation) (Record, error) {
	s.check(c, "finish")
	return s.m.Finish(c, r, o)
}
func (s *sentinelPorts) Fence(c context.Context, i Intent, w bool) error {
	s.check(c, "fence")
	return s.m.Fence(c, i, w)
}
func (s *sentinelPorts) Admit(c context.Context, i Intent, w bool) error {
	s.check(c, "authority")
	if s.deny {
		return ErrDenied
	}
	return nil
}
func (s *sentinelPorts) Apply(c context.Context, i Intent, p Plan, g Guard) Observation {
	s.check(c, "apply")
	s.guard = g
	if g(context.Background()) != nil {
		return Observation{Outcome: Denied}
	}
	switch i.Operation {
	case TransferGraph:
		return Observation{Outcome: Confirmed, Head: i.Head}
	case UploadBlob, UploadTree, UploadCommit:
		return Observation{Outcome: Confirmed, Head: p.Object.OID}
	default:
		return Observation{Outcome: Confirmed, Head: i.Head}
	}
}
func (s *sentinelPorts) Reconcile(c context.Context, i Intent, p Plan, g Guard) Observation {
	s.check(c, "reconcile")
	return s.Apply(c, i, p, g)
}
func TestCustodyExactContextEveryEffect(t *testing.T) {
	p := objectPlan(t)
	effect := context.WithValue(context.Background(), struct{}{}, "private sentinel")
	s := &sentinelSource{p: p, ctx: effect, duplicate: true}
	ports := &sentinelPorts{t: t, ctx: effect, calls: map[string]int{}}
	e := NewEngine(ports, s, ports, ports)
	if _, err := e.Execute(context.Background(), p.Reference, Publish); err != nil {
		t.Fatal(err)
	}
	if s.calls != 1 {
		t.Fatal("source reacquired")
	}
	for _, n := range []string{"reserve", "finish", "fence", "authority", "apply"} {
		want := len(p.Objects) + 2
		if n == "fence" || n == "authority" {
			want *= 2
		}
		if ports.calls[n] != want {
			t.Fatalf("%s calls=%d", n, ports.calls[n])
		}
	}
	if ports.guard(effect) == nil {
		t.Fatal("retained adapter guard gave authority")
	}
	before := ports.calls["reserve"]
	if s.saved(effect, p) == nil || ports.calls["reserve"] != before {
		t.Fatal("callback after source return gave authority")
	}
}
func TestCustodyExpiredOrDeniedPreventsAdapter(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{true: "expired", false: "denied"}[expired], func(t *testing.T) {
			p := fixturePlan()
			effect, cancel := context.WithCancel(context.Background())
			defer cancel()
			if expired {
				cancel()
			}
			s := &sentinelSource{p: p, ctx: effect}
			ports := &sentinelPorts{t: t, ctx: effect, calls: map[string]int{}, deny: true}
			_, err := NewEngine(ports, s, ports, ports).Execute(context.Background(), p.Reference, Publish)
			if expired && err == nil {
				t.Fatal("expired context accepted")
			}
			if ports.calls["apply"] != 0 {
				t.Fatal("denied proof reached adapter")
			}
		})
	}
}
