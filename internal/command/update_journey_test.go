package command

import (
	"context"
	"crypto/sha256"
	selfupdate "github.com/berryhill/aegis/internal/update"
	"github.com/berryhill/aegis/internal/userservice"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeJournalResumeAndSubstitutionDenial(t *testing.T) {
	state := t.TempDir()
	plan := userservice.Plan{UnitDigest: "unit"}
	id := selfupdate.CandidateIdentity{Executable: "/exact/aegis", Version: "1.0.0", Source: "source", Digest: "digest"}
	d := sha256.Sum256([]byte("config"))
	j, err := openUpgradeJournal(plan, state, d, id)
	if err != nil {
		t.Fatal(err)
	}
	j.receipt.Stage = "binary_install"
	if err = j.save(); err != nil {
		t.Fatal(err)
	}
	if second, e := openUpgradeJournal(plan, state, d, id); e == nil {
		second.close()
		t.Fatal("concurrent operation admitted")
	}
	j.close()
	j, err = openUpgradeJournal(plan, state, d, id)
	if err != nil {
		t.Fatal(err)
	}
	defer j.close()
	if j.receipt.Stage != "binary_install" {
		t.Fatal(j.receipt)
	}
	path := filepath.Join(state, "upgrade", "journal.json")
	other := filepath.Join(state, "other")
	if err = os.Link(path, other); err != nil {
		t.Fatal(err)
	}
	if err = j.save(); err == nil {
		t.Fatal("hardlinked journal accepted")
	}
}
func TestUpgradeJournalConflictingIntentDenied(t *testing.T) {
	state := t.TempDir()
	plan := userservice.Plan{UnitDigest: "unit"}
	id := selfupdate.CandidateIdentity{Executable: "/exact/aegis", Version: "1", Source: "source", Digest: "digest"}
	j, err := openUpgradeJournal(plan, state, [32]byte{}, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = j.save(); err != nil {
		t.Fatal(err)
	}
	j.close()
	id.Digest = "different"
	if j, e := openUpgradeJournal(plan, state, [32]byte{}, id); e == nil {
		j.close()
		t.Fatal("conflicting candidate admitted")
	}
}
func TestUpgradeConsoleReadbackAndOffOriginDenial(t *testing.T) {
	for _, mode := range []string{"ok", "bad", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "bad" {
					http.Error(w, "bad", 503)
					return
				}
				if mode == "redirect" {
					http.Redirect(w, r, "https://example.invalid/", 302)
					return
				}
				w.Write([]byte(upgradeConsoleHTML(false)))
			}))
			defer server.Close()
			err := verifyUpgradeConsole(context.Background(), server.URL)
			if (err == nil) != (mode == "ok") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}
