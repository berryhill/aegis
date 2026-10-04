package orchestration

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/loop"
)

func TestProtectedWorkspace(t *testing.T) {
	base := t.TempDir()
	state := filepath.Join(base, "state")
	sibling := filepath.Join(base, "state2")
	for _, p := range []string{state, sibling} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(state, alias); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(base, "dangling")
	if err := os.Symlink(filepath.Join(base, "absent"), dangling); err != nil {
		t.Fatal(err)
	}
	cycle := filepath.Join(base, "cycle")
	if err := os.Symlink(cycle, cycle); err != nil {
		t.Fatal(err)
	}
	regular := filepath.Join(base, "regular")
	if err := os.WriteFile(regular, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, workspace     string
		writable, protected []string
		denied              bool
	}{
		{"empty protection", sibling, []string{"file"}, nil, true},
		{"protected workspace", state, []string{"file"}, []string{state}, true},
		{"component sibling", sibling, []string{"file"}, []string{state}, false},
		{"ancestor harmless sibling", base, []string{"state2/file"}, []string{state}, false},
		{"ancestor selected protected", base, []string{"state/file"}, []string{state}, true},
		{"workspace alias", alias, []string{"file"}, []string{state}, true},
		{"selected alias missing leaf", base, []string{"alias/new/file"}, []string{state}, true},
		{"missing protected lexical", base, []string{"future/file"}, []string{filepath.Join(base, "future")}, true},
		{"missing protected alias", state, []string{"new/file"}, []string{filepath.Join(alias, "new")}, true},
		{"selected ancestor", base, []string{"state"}, []string{filepath.Join(state, "custody")}, true},
		{"relative protection", sibling, []string{"file"}, []string{"state"}, true},
		{"root protection", sibling, []string{"file"}, []string{"/"}, true},
		{"unclean protection", sibling, []string{"file"}, []string{base + "/state/../state"}, true},
		{"relative workspace", "relative", []string{"file"}, []string{state}, true},
		{"root workspace", "/", []string{"file"}, []string{state}, true},
		{"escape selected", sibling, []string{"../state/file"}, []string{state}, true},
		{"absolute selected", sibling, []string{state}, []string{state}, true},
		{"dangling selected", base, []string{"dangling/file"}, []string{state}, true},
		{"dangling protected", sibling, []string{"file"}, []string{dangling}, true},
		{"cycle selected", base, []string{"cycle/file"}, []string{state}, true},
		{"cycle protected", sibling, []string{"file"}, []string{cycle}, true},
		{"not directory selected", base, []string{"regular/file"}, []string{state}, true},
		{"not directory protected", sibling, []string{"file"}, []string{filepath.Join(regular, "file")}, true},
		{"protected alias", state, []string{"file"}, []string{alias}, true},
		{"missing safe workspace", filepath.Join(sibling, "future"), []string{"file"}, []string{state}, false},
		{"empty selected", sibling, nil, []string{state}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProtectedDoerWorkspace(loop.DoerContract{Workspace: tc.workspace, WritableFiles: tc.writable}, tc.protected)
			if (err != nil) != tc.denied {
				t.Fatalf("error = %v, denied = %v", err, tc.denied)
			}
			if err != nil && !errors.Is(err, errProtectedDoerWorkspace) {
				t.Fatalf("missing sentinel: %v", err)
			}
		})
	}
}
