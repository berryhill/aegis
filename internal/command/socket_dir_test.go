package command

import (
	"os"
	"testing"
)

// socketTestDir avoids t.TempDir's test-name component, which can exceed
// Unix-domain socket path limits under a longer TMPDIR. MkdirTemp creates a
// private real directory; no aliases or production path checks are bypassed.
func socketTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(os.TempDir(), "sock-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove test socket directory: %v", err)
		}
	})
	return dir
}
