package badger

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

func testLogRecord(meta byte, key, value []byte) []byte {
	h := []byte{meta, 0}
	h = binary.AppendUvarint(h, uint64(len(key)))
	h = binary.AppendUvarint(h, uint64(len(value)))
	h = binary.AppendUvarint(h, 0)
	h = append(h, key...)
	h = append(h, value...)
	return binary.BigEndian.AppendUint32(h, crc32.Checksum(h, crc32.MakeTable(crc32.Castagnoli)))
}
func TestRecoveryWALStrictProof(t *testing.T) {
	key := binary.BigEndian.AppendUint64([]byte("key"), ^uint64(1))
	commit := binary.BigEndian.AppendUint64([]byte("!badger!txn"), ^uint64(1))
	record := testLogRecord(0x40, key, []byte("revoked"))
	end := testLogRecord(0x80, commit, []byte("1"))
	good := append(make([]byte, 20), record...)
	good = append(good, end...)
	for _, kind := range []string{"valid", "checksum", "unfinished", "partial", "garbage"} {
		t.Run(kind, func(t *testing.T) {
			b := append([]byte(nil), good...)
			switch kind {
			case "checksum":
				b[25] ^= 1
			case "unfinished":
				b = b[:20+len(record)]
			case "partial":
				b = b[:len(b)-2]
			}
			b = append(b, make([]byte, 4096)...)
			if kind == "garbage" {
				b[len(b)-1] = 1
			}
			p := filepath.Join(t.TempDir(), "log.mem")
			if e := os.WriteFile(p, b, 0600); e != nil {
				t.Fatal(e)
			}
			offset, e := proveLog(context.Background(), p)
			if kind == "valid" {
				if e != nil || offset != int64(len(good)) {
					t.Fatal(offset, e)
				}
			} else if e == nil {
				t.Fatal("accepted damaged WAL")
			}
		})
	}
}
func TestRecoveryResumesOnlyProvenPublication(t *testing.T) {
	for _, kind := range []string{"valid", "wrong-principal", "changed-evidence"} {
		t.Run(kind, func(t *testing.T) {
			root, g := interruptedFixture(t)
			result, e := RecoverInterruptedAs(context.Background(), root, "owner")
			if e != nil {
				t.Fatal(e)
			}
			p := filepath.Join(root, "recovery-evidence", "receipt.json")
			b, e := os.ReadFile(p)
			if e != nil {
				t.Fatal(e)
			}
			var r recoveryReceipt
			if e = json.Unmarshal(b, &r); e != nil {
				t.Fatal(e)
			}
			r.Phase = "verified"
			r.Outcome = "pending"
			if e = saveRecoveryReceipt(root, r); e != nil {
				t.Fatal(e)
			}
			if e = writeMarker(root, "DIRTY", g); e != nil {
				t.Fatal(e)
			}
			principal := "owner"
			if kind == "wrong-principal" {
				principal = "other"
			}
			if kind == "changed-evidence" {
				if e = os.WriteFile(filepath.Join(result.EvidencePath, "store", "extra"), []byte("x"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			resumed, e := RecoverInterruptedAs(context.Background(), root, principal)
			if kind == "valid" {
				if e != nil || !resumed.Recovered {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("unproven resume")
			}
		})
	}
}
func TestRecoveryCopyCancellationAndSparse(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "source")
	f, e := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(32 << 20); e != nil {
		t.Fatal(e)
	}
	f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = recoveryCopy(ctx, p, filepath.Join(d, "cancelled")); e == nil {
		t.Fatal("ignored cancellation")
	}
	if _, e = recoveryCopy(context.Background(), p, filepath.Join(d, "copy")); e != nil {
		t.Fatal(e)
	}
}
