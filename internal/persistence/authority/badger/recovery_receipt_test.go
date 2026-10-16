package badger

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoveryPublicationFaultRetries(t *testing.T) {
	for _, stage := range []string{"receipt", "before-exchange", "exchange", "fsync", "before-clean", "clean", "dirty-removed", "before-completion", "completed"} {
		t.Run(stage, func(t *testing.T) {
			root, _ := interruptedFixture(t)
			hit := false
			ctx := context.WithValue(context.Background(), recoveryFaultKey{}, func(s string) error {
				if s == stage {
					hit = true
					return errors.New("injected interruption")
				}
				return nil
			})
			result, e := RecoverInterruptedAs(ctx, root, "owner")
			if !hit || e == nil || result.Recovered {
				t.Fatalf("fault not observed: %v %+v %v", hit, result, e)
			}
			if stage != "completed" {
				if inspection := Inspect(context.Background(), root); inspection.State == StateReady {
					t.Fatal("incomplete recovery was advertised as ready")
				}
				if opened, err := Open(context.Background(), root); err == nil {
					_ = opened.Close()
					t.Fatal("ordinary writer bypassed incomplete recovery")
				}
			}
			result, e = RecoverInterruptedAs(context.Background(), root, "owner")
			if e != nil || !result.Recovered {
				t.Fatalf("retry: %+v %v", result, e)
			}
			verified, e := os.ReadFile(filepath.Join(result.EvidencePath, "verified.json"))
			if e != nil {
				t.Fatal(e)
			}
			completed, e := os.ReadFile(filepath.Join(result.EvidencePath, "completed.json"))
			if e != nil {
				t.Fatal(e)
			}
			retry, e := RecoverInterruptedAs(context.Background(), root, "owner")
			if e != nil || !retry.Recovered || retry.EvidencePath != result.EvidencePath {
				t.Fatalf("idempotent readback: %+v %v", retry, e)
			}
			again, _ := os.ReadFile(filepath.Join(result.EvidencePath, "verified.json"))
			if !bytes.Equal(verified, again) {
				t.Fatal("history overwritten")
			}
			// A later actual open changes engine bytes; old completed history cannot strand a new recovery.
			store, e := Open(context.Background(), root)
			if e != nil {
				t.Fatal(e)
			}
			if e = store.Close(); e != nil {
				t.Fatal(e)
			}
			if e = os.Rename(filepath.Join(root, "CLEAN"), filepath.Join(root, "DIRTY")); e != nil {
				t.Fatal(e)
			}
			next, e := RecoverInterruptedAs(context.Background(), root, "owner")
			if e != nil || !next.Recovered || next.EvidencePath == result.EvidencePath {
				t.Fatalf("new recovery: %+v %v", next, e)
			}
			again, _ = os.ReadFile(filepath.Join(result.EvidencePath, "completed.json"))
			if !bytes.Equal(completed, again) {
				t.Fatal("completed history overwritten")
			}
		})
	}
}

func TestRecoveryReceiptCustodyDenial(t *testing.T) {
	for _, kind := range []string{"duplicate", "unknown", "trailing", "mode", "symlink", "hardlink", "oversize", "substitute", "expired"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := interruptedFixture(t)
			ctx := context.WithValue(context.Background(), recoveryFaultKey{}, func(s string) error {
				if s == "receipt" {
					return errors.New("stop")
				}
				return nil
			})
			result, e := RecoverInterruptedAs(ctx, root, "owner")
			if e == nil {
				t.Fatal("missing interruption")
			}
			p := filepath.Join(root, "recovery-evidence", "receipt.json")
			b, e := os.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "duplicate":
				b = bytes.Replace(b, []byte("{\"principal\":"), []byte("{\"principal\":\"owner\",\"principal\":"), 1)
				e = os.WriteFile(p, b, 0600)
			case "unknown":
				b = bytes.Replace(b, []byte("{\"principal\":"), []byte("{\"unknown\":true,\"principal\":"), 1)
				e = os.WriteFile(p, b, 0600)
			case "trailing":
				e = os.WriteFile(p, append(b, ' '), 0600)
			case "mode":
				e = os.Chmod(p, 0644)
			case "symlink":
				e = os.Rename(p, p+".saved")
				if e == nil {
					e = os.Symlink(p+".saved", p)
				}
			case "hardlink":
				e = os.Link(p, p+".link")
			case "oversize":
				e = os.WriteFile(p, make([]byte, 16385), 0600)
			case "substitute":
				e = os.WriteFile(filepath.Join(result.EvidencePath, "work", "extra"), []byte("x"), 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			retryCtx := context.Background()
			if kind == "expired" {
				var cancel context.CancelFunc
				retryCtx, cancel = context.WithCancel(retryCtx)
				cancel()
			}
			denied, e := RecoverInterruptedAs(retryCtx, root, "owner")
			if e == nil || denied.Recovered {
				t.Fatalf("accepted %s", kind)
			}
			if _, e = os.Stat(filepath.Join(root, "CLEAN")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("published on denial")
			}
		})
	}
}
