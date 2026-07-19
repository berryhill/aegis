package tui

import (
	"io"
	"sync"
)

// SynchronizedWriter serializes legacy command output with typed renderer output
// while command paths are migrated to events. It is deliberately presentation-
// only and never changes command bytes.
type SynchronizedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func NewSynchronizedWriter(writer io.Writer) *SynchronizedWriter {
	return &SynchronizedWriter{writer: writer}
}
func (writer *SynchronizedWriter) Underlying() io.Writer { return writer.writer }
func (writer *SynchronizedWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.writer.Write(value)
}
