package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

var ErrInputTooLarge = errors.New("composer input exceeds configured limit")
var ErrInterrupted = errors.New("composer input interrupted")

const (
	bracketedPasteEnable  = "\x1b[?2004h"
	bracketedPasteDisable = "\x1b[?2004l"
)

type Composer struct {
	mu              sync.Mutex
	input           io.Reader
	output          io.Writer
	maximum         int
	history         []string
	historyBytes    int
	maxHistory      int
	maxHistoryBytes int
}

func NewComposer(input io.Reader, output io.Writer, maximum int) *Composer {
	return &Composer{input: input, output: output, maximum: maximum, maxHistory: 100, maxHistoryBytes: 256 << 10}
}

// Read reads one submission. On a real terminal rich mode uses a restorable raw
// editor; non-file injected readers retain the existing bounded line contract.
func (composer *Composer) Read(ctx context.Context, prompt string, capabilities Capabilities) (string, bool, error) {
	composer.mu.Lock()
	defer composer.mu.Unlock()
	file, ok := composer.input.(*os.File)
	if !ok {
		return readPlain(ctx, composer.input, composer.output, prompt, composer.maximum)
	}
	if capabilities.Profile != RichInteractive || !term.IsTerminal(int(file.Fd())) {
		return readPlainFile(ctx, file, composer.output, prompt, composer.maximum)
	}
	state, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return "", false, err
	}
	defer term.Restore(int(file.Fd()), state)
	_, _ = io.WriteString(composer.output, bracketedPasteEnable)
	defer func() { _, _ = io.WriteString(composer.output, bracketedPasteDisable) }()
	_, _ = io.WriteString(composer.output, prompt+" (Enter submit; Ctrl+J newline; ? help; Ctrl-D exit) ")
	buffer := make([]byte, 0, min(composer.maximum, 4096))
	one := make([]byte, 1)
	historyIndex := len(composer.history)
	paste := false
	pasteStart := 0
	pasteLastCR := false
	for {
		if err := ctx.Err(); err != nil {
			if paste {
				drainBracketedPaste(file)
			}
			wipe(buffer)
			return "", false, err
		}
		count, readErr := readByteContext(ctx, file, one)
		if count == 0 && readErr != nil {
			if paste {
				drainBracketedPaste(file)
			}
			wipe(buffer)
			if errors.Is(readErr, io.EOF) {
				return "", true, nil
			}
			return "", false, readErr
		}
		value := one[0]
		if paste {
			if value == 0x1b {
				sequence := readEscapeSequence(ctx, file)
				if sequence == "[201~" {
					paste = false
					pasteLastCR = false
					_, _ = fmt.Fprintf(composer.output, "[paste: %d bytes guarded on submit]", len(buffer)-pasteStart)
					continue
				}
				pasted := append([]byte{value}, sequence...)
				if len(buffer)+len(pasted) > composer.maximum {
					wipe(buffer)
					wipe(pasted)
					return "", false, ErrInputTooLarge
				}
				buffer = append(buffer, pasted...)
				wipe(pasted)
				pasteLastCR = false
				continue
			}
			if value == '\n' && pasteLastCR {
				pasteLastCR = false
				continue
			}
			pasteLastCR = value == '\r'
			if value == '\r' {
				value = '\n'
			}
			if len(buffer) >= composer.maximum {
				wipe(buffer)
				return "", false, ErrInputTooLarge
			}
			buffer = append(buffer, value)
			continue
		}
		if value == 0x04 && len(buffer) == 0 {
			_, _ = io.WriteString(composer.output, "\r\n")
			return "", true, nil
		}
		if value == 0x03 || value == 0x1b {
			if value == 0x1b {
				sequence := readEscapeSequence(ctx, file)
				if sequence == "" {
					if len(buffer) > 0 {
						wipe(buffer)
						buffer = buffer[:0]
						_, _ = io.WriteString(composer.output, "\r\n[AEGIS / authoritative] input cleared\r\n")
						continue
					}
					return "", false, ErrInterrupted
				}
				if sequence == "[200~" {
					paste = true
					pasteStart = len(buffer)
					pasteLastCR = false
					continue
				}
				if sequence == "[13;2u" {
					buffer = append(buffer, '\n')
					_, _ = io.WriteString(composer.output, "\r\n")
					continue
				}
				if sequence == "[A" && len(composer.history) > 0 {
					historyIndex = max(historyIndex-1, 0)
					buffer = append(buffer[:0], composer.history[historyIndex]...)
					redrawComposer(composer.output, prompt, buffer)
					continue
				}
				if sequence == "[B" && historyIndex < len(composer.history) {
					historyIndex++
					buffer = buffer[:0]
					if historyIndex < len(composer.history) {
						buffer = append(buffer, composer.history[historyIndex]...)
					}
					redrawComposer(composer.output, prompt, buffer)
					continue
				}
			}
			if len(buffer) > 0 {
				wipe(buffer)
				buffer = buffer[:0]
				_, _ = io.WriteString(composer.output, "\r\n[AEGIS / authoritative] input cleared\r\n")
				continue
			}
			return "", false, ErrInterrupted
		}
		if value == '\r' && !paste {
			_, _ = io.WriteString(composer.output, "\r\n")
			result := string(buffer)
			return result, false, nil
		}
		if value == '\n' || value == 0x0a || value == '\r' {
			value = '\n'
		}
		if value == 0x0c {
			redrawComposer(composer.output, prompt, buffer)
			continue
		}
		if value == '	' && strings.HasPrefix(string(buffer), "/") {
			_, _ = io.WriteString(composer.output, "\r\n")
			return "/complete " + string(buffer), false, nil
		}
		if value == 0x7f || value == 0x08 {
			if len(buffer) > 0 {
				buffer = buffer[:len(buffer)-1]
				if !paste {
					_, _ = io.WriteString(composer.output, "\b \b")
				}
			}
			continue
		}
		if value == 0x12 { // bounded reverse search: most recent containing current text
			needle := string(buffer)
			for index := len(composer.history) - 1; index >= 0; index-- {
				if strings.Contains(composer.history[index], needle) {
					buffer = append(buffer[:0], composer.history[index]...)
					redrawComposer(composer.output, prompt, buffer)
					break
				}
			}
			continue
		}
		if len(buffer) >= composer.maximum {
			wipe(buffer)
			return "", false, ErrInputTooLarge
		}
		buffer = append(buffer, value)
		if !paste {
			if value == '\n' {
				_, _ = io.WriteString(composer.output, "\r\n")
			} else {
				_, _ = composer.output.Write([]byte{value})
			}
		}
	}
}

func redrawComposer(output io.Writer, prompt string, buffer []byte) {
	_, _ = fmt.Fprintf(output, "\r\x1b[2K%s %s", prompt, Sanitize(string(buffer), DefaultSanitizeOptions(Prose)))
}

func (composer *Composer) History() []string {
	composer.mu.Lock()
	defer composer.mu.Unlock()
	return append([]string(nil), composer.history...)
}
func (composer *Composer) Remember(value string) {
	composer.mu.Lock()
	defer composer.mu.Unlock()
	composer.remember(value)
}
func (composer *Composer) remember(value string) {
	if value == "" {
		return
	}
	composer.history = append(composer.history, value)
	composer.historyBytes += len(value)
	for len(composer.history) > composer.maxHistory || composer.historyBytes > composer.maxHistoryBytes {
		composer.historyBytes -= len(composer.history[0])
		composer.history[0] = ""
		composer.history = composer.history[1:]
	}
}
func readEscapeSequence(ctx context.Context, file *os.File) string {
	buffer := make([]byte, 0, 6)
	one := make([]byte, 1)
	sequenceCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	for len(buffer) < cap(buffer) {
		if _, err := readByteContext(sequenceCtx, file, one); err != nil {
			break
		}
		buffer = append(buffer, one[0])
		if one[0] >= '@' && one[0] <= '~' && len(buffer) > 1 {
			break
		}
	}
	return string(buffer)
}
func drainBracketedPaste(file *os.File) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	const end = "\x1b[201~"
	seen := ""
	one := []byte{0}
	for {
		if _, err := readByteContext(ctx, file, one); err != nil {
			return
		}
		seen += string(one)
		if len(seen) > len(end) {
			seen = seen[len(seen)-len(end):]
		}
		if strings.HasSuffix(seen, end) {
			return
		}
	}
}
func readPlain(ctx context.Context, input io.Reader, output io.Writer, prompt string, maximum int) (string, bool, error) {
	_, _ = io.WriteString(output, prompt)
	buffer := make([]byte, 0, min(maximum, 4096))
	one := make([]byte, 1)
	for {
		if err := ctx.Err(); err != nil {
			wipe(buffer)
			return "", false, err
		}
		count, err := input.Read(one)
		if count == 1 {
			if one[0] == '\n' {
				return strings.TrimSuffix(string(buffer), "\r"), false, nil
			}
			if len(buffer) >= maximum {
				wipe(buffer)
				return "", false, ErrInputTooLarge
			}
			buffer = append(buffer, one[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(buffer) == 0 {
					return "", true, nil
				}
				return string(buffer), false, nil
			}
			wipe(buffer)
			return "", false, err
		}
	}
}
func readPlainFile(ctx context.Context, file *os.File, output io.Writer, prompt string, maximum int) (string, bool, error) {
	_, _ = io.WriteString(output, prompt)
	buffer := make([]byte, 0, min(maximum, 4096))
	one := make([]byte, 1)
	for {
		count, err := readByteContext(ctx, file, one)
		if count == 1 {
			if one[0] == '\n' {
				return strings.TrimSuffix(string(buffer), "\r"), false, nil
			}
			if len(buffer) >= maximum {
				wipe(buffer)
				return "", false, ErrInputTooLarge
			}
			buffer = append(buffer, one[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(buffer) == 0 {
					return "", true, nil
				}
				return string(buffer), false, nil
			}
			wipe(buffer)
			return "", false, err
		}
	}
}
func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
