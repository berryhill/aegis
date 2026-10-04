package store

import (
	"bytes"

	"encoding/json"
	"fmt"
	"github.com/berryhill/aegis/internal/hostapproval"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDoerHostApprovalRenewalHistoryValidation(t *testing.T) {
	for _, mode := range []string{"permission", "hardlink", "symlink", "noncanonical", "signature", "generation", "predecessor", "scope", "missing", "gap", "directory-permission", "key-missing", "conflicting-payload"} {
		t.Run(mode, func(t *testing.T) {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			r := hostRecord(t)
			old, err := s.SaveDoerHostApproval(r)
			if err != nil {
				t.Fatal(err)
			}
			r.ApprovedAt = r.ExpiresAt
			r.ExpiresAt = r.ApprovedAt.Add(time.Hour)
			current, err := s.SaveDoerHostApproval(r)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(s.Root(), "doer-host-approvals")
			slot := hostapproval.Slot(r.DeploymentID, r.OwnerID, r.Agent, r.ContractDigest)
			historical := filepath.Join(dir, slot+".json")
			latest := filepath.Join(dir, hostApprovalGenerationName(slot, current.Generation))
			switch mode {
			case "permission":
				err = os.Chmod(historical, 0644)
			case "hardlink":
				err = os.Link(historical, filepath.Join(s.Root(), "historical-hardlink"))
			case "symlink":
				target := filepath.Join(s.Root(), "historical-real")
				err = os.Rename(historical, target)
				if err == nil {
					err = os.Symlink(target, historical)
				}
			case "noncanonical":
				raw, e := os.ReadFile(historical)
				if e != nil {
					t.Fatal(e)
				}
				err = os.WriteFile(historical, append(raw, '\n'), 0600)
			case "signature":
				old.Signature[0] ^= 1
				raw, _ := json.Marshal(old)
				err = os.WriteFile(historical, raw, 0600)
			case "generation", "predecessor", "scope":
				key, e := s.hostApprovalKey(false)
				if e != nil {
					t.Fatal(e)
				}
				if mode == "generation" {
					current.Generation++
				}
				if mode == "predecessor" {
					current.ApprovalPreviousDigest = "sha256:" + strings.Repeat("b", 64)
				}
				if mode == "scope" {
					current.DeploymentID = "other"
				}
				current, e = hostapproval.Sign(current, key)
				if e != nil {
					t.Fatal(e)
				}
				raw, _ := json.Marshal(current)
				err = os.WriteFile(latest, raw, 0600)
			case "missing":
				err = os.Remove(historical)
			case "gap":
				err = os.Rename(latest, filepath.Join(dir, hostApprovalGenerationName(slot, 2)))
			case "directory-permission":
				err = os.Chmod(dir, 0755)
			case "key-missing":
				err = os.Remove(filepath.Join(s.checkpointRoot, "doer-host-write-ed25519.key"))
			case "conflicting-payload":
				// Explicit generation recovery must not substitute a conflicting payload.
				conflict := current
				conflict.ExpiresAt = conflict.ExpiresAt.Add(time.Minute)
				if _, e := s.SaveDoerHostApproval(conflict); e == nil {
					t.Fatal("conflicting payload recovered")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, e := s.ReadDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ApprovedAt); e == nil {
				t.Fatal("invalid history authorized")
			}
			if _, e := s.SaveDoerHostApproval(r); e == nil {
				t.Fatal("invalid history renewed/reused")
			}
		})
	}
}

func TestDoerHostApprovalHistoryBound(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := hostRecord(t)
	first, err := s.SaveDoerHostApproval(r)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.hostApprovalKey(false)
	if err != nil {
		t.Fatal(err)
	}
	slot := hostapproval.Slot(r.DeploymentID, r.OwnerID, r.Agent, r.ContractDigest)
	dir := filepath.Join(s.Root(), "doer-host-approvals")
	previous := first
	// Signed fixtures exercise the maximum without quadratic publication overhead.
	for i := 1; i < hostapproval.MaxGenerations; i++ {
		r.Generation = uint64(i)
		r.ApprovalPreviousDigest = previous.SignedDigest()
		r.ApprovedAt = previous.ExpiresAt
		r.ExpiresAt = r.ApprovedAt.Add(time.Hour)
		previous, err = hostapproval.Sign(r, key)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(previous)
		if err = os.WriteFile(filepath.Join(dir, hostApprovalGenerationName(slot, uint64(i))), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ReadDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ApprovedAt)
	if err != nil || !bytes.Equal(got.Signature, previous.Signature) {
		t.Fatal("bounded history invalid", err)
	}
	r.Generation = 0
	r.ApprovalPreviousDigest = ""
	r.ApprovedAt = r.ExpiresAt
	r.ExpiresAt = r.ApprovedAt.Add(time.Hour)
	if _, err = s.SaveDoerHostApproval(r); err == nil {
		t.Fatal("exhausted history appended")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != hostapproval.MaxGenerations {
		t.Fatal("history pruned or appended", err)
	}
	// Even a signed extra fact beyond the bound is denied.
	raw, _ := json.Marshal(first)
	if err = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s.g000256.json", slot)), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, previous.ApprovedAt); err == nil {
		t.Fatal("excess history accepted")
	}

}
