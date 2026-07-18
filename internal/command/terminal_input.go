package command

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"sync"
)

var errTerminalLineTooLong = errors.New("terminal input exceeds the configured limit")

type terminalInput struct {
	mu     sync.Mutex
	reader *bufio.Reader
	file   *os.File
}

func newTerminalInput(input io.Reader) *terminalInput {
	t := &terminalInput{reader: bufio.NewReaderSize(input, 4096)}
	if file, ok := input.(*os.File); ok {
		t.file = file
	}
	return t
}

func (t *terminalInput) ReadLine(ctx context.Context, maximum int) (string, bool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if maximum < 1 {
		return "", false, errTerminalLineTooLong
	}
	if t.file != nil {
		return readFileLineContext(ctx, t.file, maximum)
	}
	return readBufferedLineContext(ctx, t.reader, maximum)
}

func readBufferedLineContext(ctx context.Context, reader *bufio.Reader, maximum int) (string, bool, error) {
	buffer := make([]byte, 0, min(maximum, 4096))
	for {
		if err := ctx.Err(); err != nil {
			wipeSecret(buffer)
			return "", false, err
		}
		value, err := reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(buffer) == 0 {
					return "", true, nil
				}
				return string(buffer), false, nil
			}
			wipeSecret(buffer)
			return "", false, err
		}
		if value == '\n' {
			if len(buffer) > 0 && buffer[len(buffer)-1] == '\r' {
				buffer = buffer[:len(buffer)-1]
			}
			return string(buffer), false, nil
		}
		if len(buffer) >= maximum {
			wipeSecret(buffer)
			return "", false, errTerminalLineTooLong
		}
		buffer = append(buffer, value)
	}
}
