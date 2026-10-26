package recoveryphysical

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecoveryPhaseCosts(t *testing.T) {
	d := t.TempDir()
	os.Chmod(d, 0700)
	p := filepath.Join(d, "sparse.vlog")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(256 << 20); e != nil {
		t.Fatal(e)
	}
	f.Close()
	start := time.Now()
	if _, e = Copy(context.Background(), p, filepath.Join(d, "copy")); e != nil {
		t.Fatal(e)
	}
	t.Logf("copy+logical SHA256 256MiB: %s", time.Since(start))
	start = time.Now()
	if _, e = Copy(context.Background(), p, ""); e != nil {
		t.Fatal(e)
	}
	t.Logf("logical SHA256 256MiB: %s", time.Since(start))
	start = time.Now()
	if _, e = ProveLog(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	t.Logf("WAL prove+zero suffix 256MiB: %s", time.Since(start))
}
