package recoveryphysical

import (
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// LogicalReader reads every logical byte, synthesizing only filesystem-proven
// holes. Unlike reading a multi-GiB sparse file, this does not fault zero pages
// into the page cache. Callers must retain their before/after identity checks:
// extent discovery is not an integrity proof and never replaces a digest.
func LogicalReader(f *os.File) io.Reader { return &logicalReader{f: f} }

type logicalReader struct {
	f        *os.File
	offset   int64
	dataEnd  int64
	holeEnd  int64
	fallback bool
}

func (r *logicalReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	info, err := r.f.Stat()
	if err != nil {
		return 0, err
	}
	if r.offset >= info.Size() {
		return 0, io.EOF
	}
	if int64(len(p)) > info.Size()-r.offset {
		p = p[:info.Size()-r.offset]
	}
	if !r.fallback && r.offset >= r.dataEnd && r.offset >= r.holeEnd {
		data, e := unix.Seek(int(r.f.Fd()), r.offset, unix.SEEK_DATA)
		if errors.Is(e, unix.ENXIO) {
			r.holeEnd = info.Size()
		} else if e != nil {
			r.fallback = true
		} else if data > r.offset {
			r.holeEnd = data
		} else {
			end, e := unix.Seek(int(r.f.Fd()), r.offset, unix.SEEK_HOLE)
			if e != nil {
				r.fallback = true
			} else {
				r.dataEnd = end
			}
		}
	}
	if !r.fallback && r.offset < r.holeEnd {
		if int64(len(p)) > r.holeEnd-r.offset {
			p = p[:r.holeEnd-r.offset]
		}
		clear(p)
		r.offset += int64(len(p))
		return len(p), nil
	}
	if !r.fallback && r.dataEnd > r.offset && int64(len(p)) > r.dataEnd-r.offset {
		p = p[:r.dataEnd-r.offset]
	}
	n, e := r.f.ReadAt(p, r.offset)
	r.offset += int64(n)
	return n, e
}
