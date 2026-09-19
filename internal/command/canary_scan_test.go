package command

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// streamContainsCanary scans every byte without materializing preallocated
// Badger value logs (roughly 2 GiB each). Keep overlap so chunk boundaries
// cannot hide plaintext. This changes only test scanning, not storage options.
func streamContainsCanary(r io.Reader, canary []byte) (bool, error) {
	if len(canary) == 0 {
		return true, nil
	}
	buf := make([]byte, 64*1024+len(canary)-1)
	carry := 0
	for {
		n, err := io.ReadFull(r, buf[carry:])
		end := carry + n
		if bytes.Contains(buf[:end], canary) {
			return true, nil
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		carry = min(len(canary)-1, end)
		copy(buf, buf[end-carry:end])
	}
}

func TestStreamContainsCanary(t *testing.T) {
	canary := []byte("synthetic-canary")
	for _, offset := range []int{0, 65535, 65536, 65548, 131072} {
		body := append(bytes.Repeat([]byte{'x'}, offset), canary...)
		found, err := streamContainsCanary(bytes.NewReader(body), canary)
		if err != nil || !found {
			t.Fatalf("offset %d: found=%v err=%v", offset, found, err)
		}
	}
	for _, body := range [][]byte{nil, []byte("synthetic-canar"), bytes.Repeat([]byte{'x'}, 200000)} {
		found, err := streamContainsCanary(bytes.NewReader(body), canary)
		if err != nil || found {
			t.Fatalf("absent: found=%v err=%v", found, err)
		}
	}
	expected := errors.New("scan read failed")
	if _, err := streamContainsCanary(failedCanaryReader{expected}, canary); !errors.Is(err, expected) {
		t.Fatalf("read error lost: %v", err)
	}
}

type failedCanaryReader struct{ err error }

func (r failedCanaryReader) Read([]byte) (int, error) { return 0, r.err }
