package manager

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

const messageStreamPrefix = `{"schema_version":"aegis.manager.response.v1","kind":"message","message":"`

// messagePreview recognizes one deliberately narrow wire prefix. It never
// releases proposal text or bytes before the response has identified itself as
// a canonical message-only envelope. A short tail remains buffered so partial
// JSON escapes, UTF-8, and surrogate pairs cannot be rendered prematurely.
type messagePreview struct {
	maximum  int
	buffer   []byte
	snapshot string
	disabled bool
}

func newMessagePreview(maximum int) *messagePreview { return &messagePreview{maximum: maximum} }

func (p *messagePreview) Released() bool { return p.snapshot != "" }

func (p *messagePreview) Feed(chunk []byte, emit func(string) error) error {
	if p.disabled || len(chunk) == 0 {
		return nil
	}
	if len(p.buffer)+len(chunk) > p.maximum {
		p.disabled = true
		return nil
	}
	p.buffer = append(p.buffer, chunk...)
	prefix := []byte(messageStreamPrefix)
	if len(p.buffer) < len(prefix) {
		if !bytes.Equal(p.buffer, prefix[:len(p.buffer)]) {
			p.disabled = true
		}
		return nil
	}
	if !bytes.Equal(p.buffer[:len(prefix)], prefix) {
		p.disabled = true
		return nil
	}
	raw, complete := streamedJSONString(p.buffer[len(prefix):])
	if !complete && len(raw) > 12 {
		raw = raw[:len(raw)-12]
	} else if !complete {
		return nil
	}
	decoded, ok := decodeJSONStringPrefix(raw)
	if !ok || !utf8.ValidString(decoded) {
		return nil
	}
	if len(decoded) > 16<<10 || len(decoded) < len(p.snapshot) || decoded[:len(p.snapshot)] != p.snapshot {
		p.disabled = true
		return nil
	}
	if decoded == p.snapshot {
		return nil
	}
	p.snapshot = decoded
	return emit(decoded)
}

func (p *messagePreview) Complete(message string, emit func(string) error) error {
	if p.disabled {
		return nil
	}
	if p.snapshot != "" && (len(message) < len(p.snapshot) || message[:len(p.snapshot)] != p.snapshot) {
		return errors.New("streamed manager message does not match completed response")
	}
	if message != p.snapshot {
		p.snapshot = message
		return emit(message)
	}
	return nil
}

func streamedJSONString(value []byte) ([]byte, bool) {
	escaped := false
	for index, item := range value {
		if escaped {
			escaped = false
			continue
		}
		if item == '\\' {
			escaped = true
			continue
		}
		if item == '"' {
			return value[:index], true
		}
	}
	return value, false
}

func decodeJSONStringPrefix(raw []byte) (string, bool) {
	for len(raw) > 0 {
		candidate := make([]byte, 0, len(raw)+2)
		candidate = append(candidate, '"')
		candidate = append(candidate, raw...)
		candidate = append(candidate, '"')
		var decoded string
		if json.Unmarshal(candidate, &decoded) == nil {
			return decoded, true
		}
		raw = raw[:len(raw)-1]
	}
	return "", true
}
