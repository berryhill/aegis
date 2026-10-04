package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/hostapproval"
)

func TestDoerHostApprovalUnknownPublicationOutcome(t *testing.T) {
	for _, mode := range []string{"committed", "uncommitted", "substituted", "key-changed"} {
		t.Run(mode, func(t *testing.T) {
			s, err := Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			r := hostRecord(t)
			if _, err = s.SaveDoerHostApproval(r); err != nil {
				t.Fatal(err)
			}
			r.ApprovedAt = r.ExpiresAt
			r.ExpiresAt = r.ApprovedAt.Add(time.Hour)
			unknown := errors.New("injected unknown durable publication outcome")
			got, err := s.saveDoerHostApproval(r, func(name string, raw []byte) error {
				if mode == "uncommitted" {
					return unknown
				}
				if mode == "substituted" {
					substituted, e := hostapproval.Decode(raw)
					if e != nil {
						t.Fatal(e)
					}
					substituted.ExpiresAt = substituted.ExpiresAt.Add(time.Minute)
					key, e := s.hostApprovalKey(false)
					if e != nil {
						t.Fatal(e)
					}
					substituted, e = hostapproval.Sign(substituted, key)
					if e != nil {
						t.Fatal(e)
					}
					// Encode via the purpose-specific canonical shape.
					raw, e = json.Marshal(substituted)
					if e != nil {
						t.Fatal(e)
					}
				}
				if e := writeBytesCreateOnly(name, raw); e != nil {
					return e
				}
				if mode == "key-changed" {
					if e := os.WriteFile(filepath.Join(s.checkpointRoot, "doer-host-write-ed25519.key"), make([]byte, 64), 0600); e != nil {
						t.Fatal(e)
					}
				}
				return unknown
			})
			if mode != "committed" {
				if err == nil {
					t.Fatal("unknown publication substituted authority")
				}
				return
			}
			if err != nil {
				t.Fatal("committed exact generation not recovered", err)
			}
			retry, err := s.SaveDoerHostApproval(r)
			if err != nil || !bytes.Equal(got.Signature, retry.Signature) {
				t.Fatal("retry duplicated generation", err)
			}
			entries, err := os.ReadDir(filepath.Join(s.Root(), "doer-host-approvals"))
			if err != nil || len(entries) != 2 {
				t.Fatal("retry appended duplicate", err)
			}
		})
	}
}
