package store

import (
	"github.com/berryhill/aegis/internal/hostapproval"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/reference"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hostRecord(t *testing.T) hostapproval.Record {
	t.Helper()
	now := time.Now().UTC()
	c := loop.DoerContract{Task: "Write result", Workspace: "/project", WritableFiles: []string{"out"}, VerifyFile: "out", MaxAttempts: 1}
	d, _ := c.Digest()
	v, _, e := loop.NewDoerRevision("test", 1, "", c)
	if e != nil {
		t.Fatal(e)
	}
	return hostapproval.Record{Purpose: hostapproval.Purpose, DeploymentID: "deployment", PrincipalID: "owner", OwnerID: "owner", Agent: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "agent", Revision: 1, Digest: "sha256:" + strings.Repeat("a", 64)}, Candidate: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: "test", Revision: 1, Digest: v.Digest}, Contract: c, ContractDigest: d, ApprovedAt: now, ExpiresAt: now.Add(time.Hour)}
}
func TestDoerHostApprovalCustody(t *testing.T) {
	s, e := Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	r := hostRecord(t)
	read := func() error {
		_, e := s.ReadDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ApprovedAt)
		return e
	}
	if read() == nil {
		t.Fatal("missing granted")
	}
	if _, e = os.Stat(filepath.Join(s.checkpointRoot, "doer-host-write-ed25519.key")); !os.IsNotExist(e) {
		t.Fatal("read initialized key")
	}
	signed, e := s.SaveDoerHostApproval(r)
	if e != nil {
		t.Fatal(e)
	}
	repeated, e := s.SaveDoerHostApproval(r)
	if e != nil || repeated.KeyID != signed.KeyID || !repeated.ApprovedAt.Equal(signed.ApprovedAt) {
		t.Fatal("repeat changed approval", e)
	}
	reopened, e := Open(s.Root())
	if e != nil {
		t.Fatal(e)
	}
	s = reopened
	if read() != nil {
		t.Fatal("restart denied")
	}
	slot := hostapproval.Slot(r.DeploymentID, r.OwnerID, r.Agent, r.ContractDigest)
	// Ordinary unsigned facts cannot become host authority, even at exact slot.
	name := filepath.Join(s.Root(), "doer-host-approvals", slot+".json")
	if e = os.WriteFile(name, []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	if read() == nil {
		t.Fatal("unsigned granted")
	}
	if e = os.Remove(name); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(filepath.Join(s.checkpointRoot, "doer-host-write-ed25519.key"), name); e != nil {
		t.Fatal(e)
	}
	if read() == nil {
		t.Fatal("symlink granted")
	}
}
func TestDoerHostApprovalKeyValidation(t *testing.T) {
	for _, mode := range []string{"permission", "symlink", "tamper", "missing", "hardlink"} {
		t.Run(mode, func(t *testing.T) {
			s, e := Open(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			r := hostRecord(t)
			if _, e = s.SaveDoerHostApproval(r); e != nil {
				t.Fatal(e)
			}
			name := filepath.Join(s.checkpointRoot, "doer-host-write-ed25519.key")
			switch mode {
			case "permission":
				e = os.Chmod(name, 0644)
			case "tamper":
				e = os.WriteFile(name, make([]byte, 64), 0600)
			case "missing":
				e = os.Remove(name)
			case "symlink":
				e = os.Rename(name, name+".real")
				if e == nil {
					e = os.Symlink(name+".real", name)
				}
			case "hardlink":
				e = os.Link(name, name+".link")
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.ReadDoerHostApproval(r.DeploymentID, r.OwnerID, r.Agent, r.Contract, r.ApprovedAt); e == nil {
				t.Fatal("unsafe key authorized")
			}
		})
	}
}
