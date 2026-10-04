package api

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Commit the real signed record, then deliberately interrupt before the portal's
// ApprovalID assignment. This is not a fabricated pending status or receipt.
func TestDoerPortalRecoverySignedWriteInterruption(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(strconv.FormatBool(concurrent), func(t *testing.T) {
			p, e, cookie, csrf, d, subject, session := portalFixture(t)
			v, err := p.retain(context.Background(), subject, session, d.ID, "interrupted-run")
			if err != nil {
				t.Fatal(err)
			}
			uid, _ := strconv.ParseUint(p.svc.Config.Principal.UID, 10, 32)
			originalExecutor, err := p.svc.AuthenticateUnixPeer(context.Background(), uint32(uid))
			if err != nil {
				t.Fatal(err)
			}
			id, err := p.svc.ApproveDoerContinuationAs(context.Background(), originalExecutor, v.Review.Intent)
			if err != nil {
				t.Fatal(err)
			}
			before := continuationState(t, p.svc)
			now := p.svc.Now().Add(time.Second)
			p.svc.Now = func() time.Time { return now }
			var launches atomic.Int32
			p.launch = func(context.Context, string, func(int)) string { launches.Add(1); return "unknown" }
			n := 1
			if concurrent {
				n = 12
			}
			var wg sync.WaitGroup
			for j := 0; j < n; j++ {
				wg.Add(1)
				go func(j int) {
					defer wg.Done()
					action := "request-local-review"
					if j%2 == 1 {
						action = "observe"
					}
					out := portalPost(e, cookie, csrf, url.Values{"action": {action}, "intent_id": {v.ID}})
					if out.Code != 200 {
						t.Errorf("recovery: %d %s", out.Code, out.Body.String())
					}
				}(j)
			}
			wg.Wait()
			recovered, err := p.reload(context.Background(), v.ID)
			if err != nil || recovered.ApprovalID != id || recovered.Status != "approved" {
				t.Fatalf("lost signed approval linkage: %+v %v", recovered, err)
			}
			signed, err := p.svc.Store.ReadDoerContinuation(id)
			if err != nil || !reflect.DeepEqual(signed.Executor, originalExecutor) || !signed.Executor.AuthenticatedAt.Equal(originalExecutor.AuthenticatedAt) || !signed.Intent.ExpiresAt.Equal(v.Review.Intent.ExpiresAt) {
				t.Fatal("recovery refreshed original authority", err)
			}
			if launches.Load() != 0 {
				t.Fatalf("recovery launched %d new confirmations", launches.Load())
			}
			if !reflect.DeepEqual(before, continuationState(t, p.svc)) {
				t.Fatal("recovery mutated signed bytes, key, or canonical state")
			}
		})
	}
}

func TestDoerPortalRecoveryCorruptExistingDeniesWithoutDialog(t *testing.T) {
	for _, damage := range []string{"record", "missing-key", "intent-mismatch"} {
		t.Run(damage, func(t *testing.T) {
			p, e, cookie, csrf, d, subject, session := portalFixture(t)
			v, err := p.retain(context.Background(), subject, session, d.ID, "corrupt-run")
			if err != nil {
				t.Fatal(err)
			}
			uid, _ := strconv.ParseUint(p.svc.Config.Principal.UID, 10, 32)
			executor, err := p.svc.AuthenticateUnixPeer(context.Background(), uint32(uid))
			if err != nil {
				t.Fatal(err)
			}
			id, err := p.svc.ApproveDoerContinuationAs(context.Background(), executor, v.Review.Intent)
			if err != nil {
				t.Fatal(err)
			}
			if damage == "record" {
				err = os.WriteFile(filepath.Join(p.svc.Store.Root(), "controller-continuation", "approvals", id+".json"), []byte("{}"), 0600)
			} else if damage == "missing-key" {
				err = os.Remove(filepath.Join(p.svc.Store.Root(), "controller-continuation", "ed25519-seed"))
			} else {
				p.pending[v.ID].Review.Intent.ExpiresAt = v.Review.Intent.ExpiresAt.Add(-time.Second)
				p.pending[v.ID].Review.Digest = p.pending[v.ID].Review.ContentDigest()
			}
			if err != nil {
				t.Fatal(err)
			}
			var launches atomic.Int32
			p.launch = func(context.Context, string, func(int)) string { launches.Add(1); return "unknown" }
			for j := 0; j < 2; j++ {
				out := portalPost(e, cookie, csrf, url.Values{"action": {"request-local-review"}, "intent_id": {v.ID}})
				if out.Code == 200 {
					t.Fatal("corrupt existing approval accepted")
				}
			}
			if launches.Load() != 0 {
				t.Fatal("corruption launched repeated dialog")
			}
		})
	}
}
