package api

import (
	"github.com/berryhill/aegis/internal/app"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The installed fixture already owns its real listener and stores.
func queueRequeueBrowser(t *testing.T, svc *app.Service, id string) {
	t.Helper()
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := os.MkdirTemp(filepath.Join(repo, ".candidate", "issue332-completion"), "requeue-browser-")
	if err != nil {
		t.Fatal(err)
	}
	t.Log("Requeue browser evidence:", evidence)
	cmd := exec.Command("python3", filepath.Join(repo, "scripts", "queue_requeue_browser_test.py"))
	cmd.Env = append(os.Environ(), "AEGIS_CONSOLE_BASE="+svc.Config.API.Console.Origin, "AEGIS_CONSOLE_PASSWORD="+principalTestSecret(), "AEGIS_REQUEUE_ITEM="+id, "AEGIS_REQUEUE_EVIDENCE="+evidence)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal(err)
	}
}
