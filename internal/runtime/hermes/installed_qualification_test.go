package hermes

import (
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"os"
	"path/filepath"
	"testing"
)

// Metadata-only: no child, lease write, auth/config/history read, or network.
func TestInstalledNativeQualificationReadOnly(t *testing.T) {
	root := os.Getenv("AEGIS_TEST_INSTALLED_QUALIFICATION_SOURCE")
	if root == "" {
		t.Skip("explicit installed source required")
	}
	d := core.RuntimeDescriptor{Version: "0.23.0", Installation: root}
	if err := qualifyProviderTransport(d); err != nil {
		t.Fatal("exact installed qualification", err)
	}
	if !errors.Is(qualifyProviderFormat(d), ErrProviderAuthRuntimeUnqualified) {
		t.Fatal("one-POST format probe gate broadened")
	}
	for _, version := range []string{"0.23.1", "0.24.0", "0.21.3"} {
		d.Version = version
		if !errors.Is(qualifyProviderTransport(d), ErrProviderAuthRuntimeUnqualified) {
			t.Fatal("version drift accepted")
		}
	}
	t.Log("exact installed source, interpreter, locks, committed generation, workspace and dependency identities accepted; unknown/legacy-mismatch denied; no subprocess or native data")
}

func TestInstalledQualificationRecords(t *testing.T) {
	var records []installedCodexRecord
	if err := json.Unmarshal(installedCodexRecordJSON, &records); err != nil || len(records) != 2 {
		t.Fatal("record schema", err)
	}
	for _, r := range records {
		if !r.Accepted || r.Schema != 1 || r.Version != "0.23.0" || len(r.Source) != 64 || len(r.Python) != 64 || len(r.PythonPackage) != 64 || len(r.Dependencies) != 64 || len(r.WorkspaceSource) != 64 {
			t.Fatal("incomplete exact record")
		}
	}
	if records[0].Source != records[1].Source || records[0].Python != records[1].Python {
		t.Fatal("reproduction changed source/interpreter")
	}
}

func TestQualificationTreeDriftAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "native.py")
	if err := os.WriteFile(path, []byte("pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := qualificationTree(root, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "unknown.py"), []byte("pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := qualificationTree(root, false, "")
	if err != nil || before == after {
		t.Fatal("unknown source ignored", err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(root, "unknown.py"), path); err != nil {
		t.Fatal(err)
	}
	if _, err = qualificationTree(root, false, ""); !errors.Is(err, ErrProviderAuthRuntimeUnqualified) {
		t.Fatal("source symlink followed")
	}
}
