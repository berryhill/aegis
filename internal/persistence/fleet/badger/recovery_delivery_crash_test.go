package badger

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
)

// This child is also a normal-writer fixture entry point for independent public
// CLI acceptance. It deliberately exits without Close; no marker is fabricated.
func TestRecoveryDeliveryCrashChild(t *testing.T) {
	root := os.Getenv("AEGIS_DELIVERY_RECOVERY_CHILD")
	if root == "" {
		return
	}
	syscall.Umask(0077)
	ctx := context.Background()
	parent := filepath.Dir(root)
	if _, err := authoritybadger.Initialize(ctx, filepath.Join(parent, "authority-v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := authoritybadger.Open(ctx, filepath.Join(parent, "authority-v1")); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	_, c, _, _ := recoveryDeliveryFixtureStore(t, s, true)
	entries := []struct{ Key, Value []byte }{}
	for k, v := range recoveryInventory(t, s) {
		entries = append(entries, struct{ Key, Value []byte }{[]byte(k), v})
	}
	wire, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(parent, "fleet-before.json"), wire, 0600); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("AEGIS_DELIVERY_RECOVERY_MALFORMED") == "1" {
		setRawRecord(t, s, key(familyContributionPublication, "released", c.OwnerID, c.Key), []byte("{}"))
	}
	if err = s.db.Sync(); err != nil {
		t.Fatal(err)
	}
	os.Exit(0)
}

func TestRecoveryDeliveryInterrupted(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		name := "normal"
		if malformed {
			name = "malformed"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			private := t.TempDir()
			if err := os.Chmod(private, 0700); err != nil {
				t.Fatal(err)
			}
			parent := filepath.Join(private, "persistence")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(parent, "fleet-v1")
			childCtx, cancel := context.WithTimeout(ctx, time.Minute)
			defer cancel()
			child := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestRecoveryDeliveryCrashChild$")
			child.Env = append(os.Environ(), "TMPDIR="+parent, "AEGIS_DELIVERY_RECOVERY_CHILD="+root)
			if malformed {
				child.Env = append(child.Env, "AEGIS_DELIVERY_RECOVERY_MALFORMED=1")
			}
			if b, err := child.CombinedOutput(); err != nil {
				t.Fatalf("child: %v %s", err, b)
			}
			authority := filepath.Join(parent, "authority-v1")
			before, err := authoritybadger.RecoverInterruptedAs(ctx, authority, "fixture-owner")
			if err != nil {
				t.Fatal(err)
			}
			if opened, e := Open(ctx, root); e == nil {
				opened.Close()
				t.Fatal("dirty store admitted")
			}
			recovered, err := RecoverInterruptedAs(ctx, root, "fixture-owner")
			if malformed {
				if err == nil {
					t.Fatal("corruption admitted")
				}
				if _, e := os.Stat(filepath.Join(root, "CLEAN")); !os.IsNotExist(e) {
					t.Fatal("CLEAN published")
				}
				if opened, e := Open(ctx, root); e == nil {
					opened.Close()
					t.Fatal("corrupt store opened")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				again, e := RecoverInterruptedAs(ctx, root, "fixture-owner")
				if e != nil || !reflect.DeepEqual(recovered, again) {
					t.Fatalf("replay: %v", e)
				}
				s, e := Open(ctx, root)
				if e != nil {
					t.Fatal(e)
				}
				defer s.Close()
				wire, e := os.ReadFile(filepath.Join(parent, "fleet-before.json"))
				if e != nil {
					t.Fatal(e)
				}
				var entries []struct{ Key, Value []byte }
				if e = json.Unmarshal(wire, &entries); e != nil {
					t.Fatal(e)
				}
				want := map[string][]byte{}
				for _, entry := range entries {
					want[string(entry.Key)] = entry.Value
				}
				if !reflect.DeepEqual(want, recoveryInventory(t, s)) || recovered.Records != uint64(len(want)) {
					t.Fatal("logical history changed")
				}
				checkPresentationRecovery(t, s, true)
			}
			after, e := authoritybadger.RecoverInterruptedAs(ctx, authority, "fixture-owner")
			if e != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("authority receipt changed: %v", e)
			}
		})
	}
}
