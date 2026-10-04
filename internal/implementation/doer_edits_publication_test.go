package implementation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyDoerEditsDeniesAtPublicationPreservingOriginal(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "existing"}[existing], func(t *testing.T) {
			c := doerEditContract(t)
			target := filepath.Join(c.Workspace, "result.txt")
			if existing {
				if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			count := 0
			denied := &Halt{State: "denied"}
			err := ApplyDoerEdits(context.Background(), c, []Edit{{Path: "result.txt", Content: []byte("bad")}}, func(context.Context, string) error {
				count++
				if count == 2 {
					return denied
				}
				return nil
			})
			if !errors.Is(err, denied) || count != 2 {
				t.Fatalf("err=%v count=%d", err, count)
			}
			raw, readErr := os.ReadFile(target)
			if existing {
				if readErr != nil || string(raw) != "original" {
					t.Fatalf("target changed %q %v", raw, readErr)
				}
			} else if !errors.Is(readErr, os.ErrNotExist) {
				t.Fatalf("target published: %v", readErr)
			}
			files, err := filepath.Glob(filepath.Join(c.Workspace, ".aegis-doer-edit-*"))
			if err != nil || len(files) != 0 {
				t.Fatalf("temporary bytes retained: %v %v", files, err)
			}
		})
	}
}
