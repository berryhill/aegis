package badger

import (
	"context"
	"github.com/berryhill/aegis/internal/delivery"
	"strings"
	"testing"
)

func TestContributionDeliveryRootReadbackReopen(t *testing.T) {
	ctx := context.Background()
	s := newDeliveryStore(t)
	c, b, i := seedDelivery(t, s, "")
	r := deliveryRepo(t, s, b, &deliveryTestAuthority{})
	old, f, e := r.Reserve(ctx, i)
	if e != nil || !f {
		t.Fatal(e)
	}
	done, e := r.Finish(ctx, old, delivery.Observation{Outcome: delivery.Unknown, Merge: strings.Repeat("e", 40)})
	if e != nil {
		t.Fatal(e)
	}
	root := s.root
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(ctx, root)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e := s.GetContributionDeliveryHistory(ctx, c.OwnerID, c.Key)
	if e != nil {
		t.Fatal(e)
	}
	if got.RootRun != c.RunID || got.Contract.Digest != c.Digest || len(got.Histories) != 1 || got.Histories[0][1].Record != done {
		t.Fatal("canonical history lost")
	}
	if delivery.ValidateHistorySnapshot(got) != nil {
		t.Fatal("neutral snapshot rejected")
	}
	for name, mutate := range map[string]func(*delivery.HistorySnapshot){
		"root":       func(v *delivery.HistorySnapshot) { v.Root.Digest = "bad" },
		"contract":   func(v *delivery.HistorySnapshot) { v.Contract.Digest = "bad" },
		"owner":      func(v *delivery.HistorySnapshot) { v.Contract.OwnerID = "foreign" },
		"prevdigest": func(v *delivery.HistorySnapshot) { v.Histories[0][1].PreviousDigest = "bad" },
		"hash":       func(v *delivery.HistorySnapshot) { v.Histories[0][0].Digest = "bad" },
		"order": func(v *delivery.HistorySnapshot) {
			v.Histories[0][0], v.Histories[0][1] = v.Histories[0][1], v.Histories[0][0]
		},
		"duplicateintent": func(v *delivery.HistorySnapshot) { v.Histories = append(v.Histories, v.Histories[0]) },
		"outcome":         func(v *delivery.HistorySnapshot) { v.Histories[0][1].Record.Outcome = delivery.Confirmed },
	} {
		t.Run(name, func(t *testing.T) {
			v := got
			v.Histories = make([][]delivery.HistoryEnvelope, len(got.Histories))
			for n, h := range got.Histories {
				v.Histories[n] = append([]delivery.HistoryEnvelope(nil), h...)
			}
			mutate(&v)
			if delivery.ValidateHistorySnapshot(v) == nil {
				t.Fatal("malformed snapshot accepted")
			}
		})
	}
	if _, e = s.GetContributionDeliveryHistory(ctx, "foreign", c.Key); e == nil {
		t.Fatal("foreign owner accepted")
	}
}
