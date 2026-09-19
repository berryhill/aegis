package app

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/queue"
	"os"
	"path/filepath"
	"testing"
)

func TestPreparationPartitionUsesCurrentState(t *testing.T) {
	views := []QueueExecutionView{
		{Item: queue.Item{ItemID: "legacy", State: queue.StateQueued}, Projection: queue.Projection{State: queue.StateAwaitingRuntime}},
		{Item: queue.Item{ItemID: "prepared", State: queue.StatePreparationPending}, RuntimeAuthorityBinding: &queue.RuntimeBinding{}, Projection: queue.Projection{State: queue.StateQueued}},
		{Item: queue.Item{ItemID: "pending"}, Projection: queue.Projection{State: queue.StatePreparationPending}},
	}
	executable, preparation := PartitionExecutionViews(views)
	if len(executable) != 1 || executable[0].Item.ItemID != "prepared" || len(preparation) != 2 {
		t.Fatalf("wrong partition: %v %v", executable, preparation)
	}
}
func TestPreviewReceiptDiagnostics(t *testing.T) {
	s := testService(t)
	data, _ := json.Marshal(testCharter(s.Now()))
	if _, err := s.ImportCharter(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	_, d, err := s.PreviewSession(context.Background(), "office", 1, "principal", core.Environment{Name: "local"})
	if err == nil || d.Allowed || d.Reason != "provisioning_receipt_missing" {
		t.Fatalf("missing: %+v %v", d, err)
	}
	if err := os.MkdirAll(filepath.Join(s.Store.Root(), "receipts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Store.Root(), "receipts", "corrupt.json"), []byte("secret invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, d, err = s.PreviewSession(context.Background(), "office", 1, "principal", core.Environment{Name: "local"})
	if err == nil || d.Allowed || d.Reason != "provisioning_receipt_unavailable" {
		t.Fatalf("unavailable: %+v %v", d, err)
	}
}

func TestPreparationPartitionTerminalProvenance(t *testing.T) {
	for _, initial := range []queue.State{queue.StatePreparationPending, queue.StateAwaitingRuntime} {
		for _, terminal := range []queue.State{queue.StateCancelled, queue.StateRevoked} {
			v := QueueExecutionView{Item: queue.Item{State: initial}, Projection: queue.Projection{State: terminal}}
			executable, preparation := PartitionExecutionViews([]QueueExecutionView{v})
			if len(executable) != 0 || len(preparation) != 1 {
				t.Fatalf("never-bound %s/%s leaked", initial, terminal)
			}
			v.RuntimeAuthorityBinding = &queue.RuntimeBinding{}
			executable, preparation = PartitionExecutionViews([]QueueExecutionView{v})
			if len(executable) != 1 || len(preparation) != 0 {
				t.Fatalf("bound history lost")
			}
		}
	}
}
