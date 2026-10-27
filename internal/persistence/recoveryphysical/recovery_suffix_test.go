package recoveryphysical

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryZeroSuffixChunkBoundaries(t *testing.T) {
	const size = (2 << 16) + 37
	for _, offset := range []int{-1, 20, 24, (1 << 16) - 1, 1 << 16, (1 << 16) + 20, size - 1} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			data := make([]byte, size)
			if offset >= 0 {
				data[offset] = 1
			}
			path := filepath.Join(t.TempDir(), "000001.vlog")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			end, err := ProveLog(context.Background(), path)
			if offset < 0 {
				if err != nil || end != 20 {
					t.Fatalf("zero suffix: end=%d err=%v", end, err)
				}
			} else if err == nil {
				t.Fatal("nonzero suffix accepted")
			}
			actual, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(actual, data) {
				t.Fatal("proof changed original evidence", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err = ProveLog(ctx, path); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
		})
	}
}
