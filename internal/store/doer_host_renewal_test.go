package store

import (
	"bytes"
	"github.com/berryhill/aegis/internal/hostapproval"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDoerHostApprovalRenewal(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := hostRecord(t)
	old, err := s.SaveDoerHostApproval(r)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(s.Root(), "doer-host-approvals", hostapproval.Slot(r.DeploymentID, r.OwnerID, r.Agent, r.ContractDigest)+".json")
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"generation"`)) || bytes.Contains(raw, []byte(`"approval_previous_digest"`)) {
		t.Fatal("generation zero changed legacy canonical bytes")
	}
	active := r
	active.ApprovedAt = r.ApprovedAt.Add(time.Minute)
	active.ExpiresAt = active.ApprovedAt.Add(time.Hour)
	same, err := s.SaveDoerHostApproval(active)
	if err != nil || !same.ApprovedAt.Equal(old.ApprovedAt) || !same.ExpiresAt.Equal(old.ExpiresAt) {
		t.Fatalf("active consent changed: %v", err)
	}
	renewed := r
	renewed.ApprovedAt = r.ExpiresAt
	renewed.ExpiresAt = renewed.ApprovedAt.Add(time.Hour)
	if _, err := s.ReadDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, renewed.ApprovedAt); err == nil {
		t.Fatal("expired read renewed authority")
	}
	var wg sync.WaitGroup
	reopened, err := Open(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan hostapproval.Record, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		writer := s
		if i%2 != 0 {
			writer = reopened
		}
		go func(writer *Store) {
			defer wg.Done()
			got, e := writer.SaveDoerHostApproval(renewed)
			results <- got
			errs <- e
		}(writer)
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatalf("explicit renewal failed: %v", e)
		}
	}
	var first hostapproval.Record
	for got := range results {
		if first.Signature == nil {
			first = got
		}
		if !bytes.Equal(first.Signature, got.Signature) || !got.ApprovedAt.Equal(renewed.ApprovedAt) {
			t.Fatal("concurrent renewal duplicated")
		}
	}
	got, err := s.ReadDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, renewed.ApprovedAt)
	if err != nil || !bytes.Equal(got.Signature, first.Signature) {
		t.Fatal("renewal not selected", err)
	}
	after, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(after, raw) {
		t.Fatal("old signed bytes changed", err)
	}
	entries, err := os.ReadDir(filepath.Dir(name))
	if err != nil || len(entries) != 2 {
		t.Fatal("duplicate renewal history", err, len(entries))
	}
	// A retry with the original explicit request recovers the exact generation.
	retry, err := s.SaveDoerHostApproval(renewed)
	if err != nil || !bytes.Equal(retry.Signature, first.Signature) {
		t.Fatal("retry did not recover generation", err)
	}
}
