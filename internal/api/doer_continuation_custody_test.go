package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/berryhill/aegis/internal/core"
)

func TestDoerContinuationSignedCustodyTamperingDeniesExecutor(t *testing.T) {
	for _, mode := range []string{"changed-executor", "changed-key", "record-hardlink", "key-hardlink", "record-mode"} {
		t.Run(mode, func(t *testing.T) {
			svc, controller, intent := continuationFixture(t)
			id, err := svc.ApproveDoerContinuationAs(context.Background(), controller, intent)
			if err != nil {
				t.Fatal(err)
			}
			approval, err := svc.Store.ReadDoerContinuation(id)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(svc.Store.Root(), "controller-continuation", "approvals", id+".json")
			key := filepath.Join(svc.Store.Root(), "controller-continuation", "ed25519-seed")
			switch mode {
			case "changed-executor":
				approval.Executor.ID = "local-uid:0"
				wire, err := json.Marshal(approval)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, wire, 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-key":
				if err = os.WriteFile(key, make([]byte, 32), 0600); err != nil {
					t.Fatal(err)
				}
			case "record-hardlink":
				if err = os.Link(path, path+".alias"); err != nil {
					t.Fatal(err)
				}
			case "key-hardlink":
				if err = os.Link(key, key+".alias"); err != nil {
					t.Fatal(err)
				}
			case "record-mode":
				if err = os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			}
			executor, err := svc.ResolveDoerContinuationAs(context.Background(), intent.Requester, intent.SessionID, id, intent)
			if err == nil || !reflect.DeepEqual(executor, core.Subject{}) {
				t.Fatal("tampered custody emitted an executor")
			}
			result, err := svc.QueueDoerContinuationAs(context.Background(), intent.Requester, intent.SessionID, id, intent)
			if err == nil || result.QueueItemID != "" || result.Execution != nil {
				t.Fatal("tampered custody admitted executable success")
			}
		})
	}
}
