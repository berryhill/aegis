package recoveryphysical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLogicalReaderSparseDataAndSuffix(t *testing.T) {
	d := t.TempDir()
	os.Chmod(d, 0700)
	p := filepath.Join(d, "sparse.vlog")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	const size = 8 << 20
	if e = f.Truncate(size); e != nil {
		t.Fatal(e)
	}
	for _, offset := range []int64{0, 4095, 1 << 20, size - 1} {
		if _, e = f.WriteAt([]byte{0x91}, offset); e != nil {
			t.Fatal(e)
		}
	}
	want, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	got, e := io.ReadAll(LogicalReader(f))
	if e != nil || !bytes.Equal(got, want) {
		t.Fatalf("logical bytes differ: %v", e)
	}
	sum := sha256.Sum256(want)
	actual, e := Copy(context.Background(), p, filepath.Join(d, "copy"))
	if e != nil || actual != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest mismatch: %s %v", actual, e)
	}
	// A nonzero byte beyond a long sparse suffix must not disappear in proof.
	if _, e = f.WriteAt(make([]byte, 4096), 0); e != nil {
		t.Fatal(e)
	}
	if _, e = ProveLog(context.Background(), p); e == nil {
		t.Fatal("nonzero sparse suffix admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = Copy(ctx, p, ""); !errors.Is(e, context.Canceled) {
		t.Fatalf("masked cancellation: %v", e)
	}
}
