package badger

// This parser is deliberately pinned to Badger v4.9.5's unencrypted log format.
// It is stricter than Badger replay: no damaged or unfinished transaction is lost.
import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	physical "github.com/berryhill/aegis/internal/persistence/recoveryphysical"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func proveLog(ctx context.Context, path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(physical.LogicalReader(f), 64<<10)
	header := make([]byte, 20)
	if _, err = io.ReadFull(r, header); err != nil {
		return 0, err
	}
	if binary.BigEndian.Uint64(header[:8]) != 0 {
		return 0, errors.New("encrypted recovery log unsupported")
	}
	offset := int64(20)
	var pending uint64
	for {
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		first, e := r.Peek(1)
		if e == io.EOF {
			if pending != 0 {
				return 0, errors.New("incomplete WAL transaction")
			}
			return offset, nil
		}
		if e != nil {
			return 0, e
		}
		// A zero header is accepted only if EVERY remaining byte is zero.
		peek, _ := r.Peek(5)
		if len(peek) == 5 && bytes.Equal(peek, make([]byte, 5)) {
			buf := make([]byte, 64<<10)
			for {
				if e = ctx.Err(); e != nil {
					return 0, e
				}
				n, re := r.Read(buf)
				for _, b := range buf[:n] {
					if b != 0 {
						return 0, errors.New("nonzero WAL suffix")
					}
				}
				if re == io.EOF {
					break
				}
				if re != nil {
					return 0, re
				}
			}
			if pending != 0 {
				return 0, errors.New("incomplete WAL transaction")
			}
			return offset, nil
		}
		_ = first
		h := make([]byte, 0, 22)
		for i := 0; i < 2; i++ {
			b, e := r.ReadByte()
			if e != nil {
				return 0, e
			}
			h = append(h, b)
		}
		vals := [3]uint64{}
		for i := range vals {
			start := len(h)
			for j := 0; ; j++ {
				if j >= 10 {
					return 0, errors.New("WAL varint overflow")
				}
				b, e := r.ReadByte()
				if e != nil {
					return 0, e
				}
				h = append(h, b)
				if b < 128 {
					break
				}
			}
			v, n := binary.Uvarint(h[start:])
			if n <= 0 {
				return 0, errors.New("invalid WAL header")
			}
			vals[i] = v
		}
		if vals[0] < 8 || vals[0] > 65536 || vals[1] > 64<<20 {
			return 0, errors.New("invalid WAL record bounds")
		}
		kv := make([]byte, int(vals[0]+vals[1]))
		if _, e = io.ReadFull(r, kv); e != nil {
			return 0, e
		}
		crc := make([]byte, 4)
		if _, e = io.ReadFull(r, crc); e != nil {
			return 0, e
		}
		hash := crc32.New(crc32.MakeTable(crc32.Castagnoli))
		hash.Write(h)
		hash.Write(kv)
		if hash.Sum32() != binary.BigEndian.Uint32(crc) {
			return 0, errors.New("WAL checksum mismatch")
		}
		ts := ^binary.BigEndian.Uint64(kv[vals[0]-8 : vals[0]])
		switch h[0] & 0xc0 {
		case 0x40:
			if ts == 0 || (pending != 0 && ts != pending) {
				return 0, errors.New("WAL transaction mismatch")
			}
			pending = ts
		case 0x80:
			end, e := strconv.ParseUint(string(kv[vals[0]:]), 10, 64)
			if e != nil || pending == 0 || end != pending || ts != pending || !bytes.Equal(kv[:vals[0]-8], []byte("!badger!txn")) {
				return 0, errors.New("invalid WAL commit")
			}
			pending = 0
		case 0:
			if pending != 0 {
				return 0, errors.New("interleaved WAL transaction")
			}
		default:
			return 0, errors.New("invalid WAL transaction bits")
		}
		offset += int64(len(h) + len(kv) + 4)
	}
}

func proveAndTrimLogs(ctx context.Context, path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".mem") && !strings.HasSuffix(name, ".vlog") {
			continue
		}
		end, err := proveLog(ctx, filepath.Join(path, name))
		if err != nil {
			return err
		}
		// Both Badger WALs and value logs are preallocated. proveLog has
		// authenticated every record and checked EVERY byte of the zero suffix.
		// Trim only this independently preserved working copy; retaining the
		// multi-GiB sparse value-log suffix makes every later custody digest scan
		// it again and can exhaust fresh authentication before publication.
		if err = os.Truncate(filepath.Join(path, name), end); err != nil {
			return err
		}
	}
	return nil
}
