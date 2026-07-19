package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// TextContext selects the controls and layout bounds permitted for text.
type TextContext uint8

const (
	Prose TextContext = iota
	SingleLine
	SecurityField
)

// SanitizeOptions bounds rendering work after control-sequence removal.
type SanitizeOptions struct {
	Context  TextContext
	MaxBytes int
	MaxRunes int
	MaxLines int
	MaxWidth int
	MaxLinks int
}

func DefaultSanitizeOptions(context TextContext) SanitizeOptions {
	return SanitizeOptions{Context: context, MaxBytes: 256 << 10, MaxRunes: 128 << 10, MaxLines: 2_000, MaxWidth: 8_192, MaxLinks: 64}
}

// Sanitize is the central terminal-safety boundary. It removes ECMA-48/ANSI
// control strings before any width calculation or rendering and applies
// deterministic resource bounds. It never emits terminal escape sequences.
func Sanitize(input string, options SanitizeOptions) string {
	if options.MaxBytes <= 0 || options.MaxRunes <= 0 || options.MaxLines <= 0 || options.MaxWidth <= 0 {
		return "[content omitted: unsafe rendering bounds]"
	}
	if len(input) > options.MaxBytes*4 {
		input = input[:options.MaxBytes*4]
	}
	input = strings.ToValidUTF8(input, "�")
	var output strings.Builder
	output.Grow(min(len(input), options.MaxBytes))
	line, lines, runes, unsafeControls, totalRunes := 0, 1, 0, 0, 0
	omitted := false
	for index := 0; index < len(input); {
		if output.Len() >= options.MaxBytes || runes >= options.MaxRunes || lines > options.MaxLines {
			omitted = true
			break
		}
		value, size := utf8.DecodeRuneInString(input[index:])
		totalRunes++
		if value == 0x1b {
			unsafeControls++
			index = consumeEscape(input, index+size)
			continue
		}
		// C1 introducers can occur as Unicode code points after UTF-8 decoding.
		if value >= 0x80 && value <= 0x9f {
			unsafeControls++
			index += size
			if value == 0x9b {
				index = consumeCSI(input, index)
			} else if value == 0x90 || value == 0x9d || value == 0x9e || value == 0x9f || value == 0x98 {
				index = consumeStringControl(input, index)
			}
			continue
		}
		index += size
		if dangerousFormat(value) || value == '\r' || value == '\b' || value == 0x7f {
			unsafeControls++
			continue
		}
		if value == '\n' {
			if options.Context != Prose {
				value = ' '
			} else {
				lines++
				line = 0
			}
		} else if value == '\t' {
			if options.Context != Prose {
				value = ' '
			} else {
				line += 4
			}
		} else if unicode.IsControl(value) {
			unsafeControls++
			continue
		} else {
			line += runeDisplayWidth(value)
		}
		if line > options.MaxWidth {
			if options.Context == Prose {
				output.WriteRune('\n')
				lines++
				line = 1
			} else {
				omitted = true
				break
			}
		}
		if output.Len()+utf8.RuneLen(value) > options.MaxBytes {
			omitted = true
			break
		}
		output.WriteRune(value)
		runes++
	}
	result := output.String()
	if binaryLike(result) || (unsafeControls > 4 && unsafeControls*20 > max(totalRunes, 1)) {
		return "[binary-like content omitted]"
	}
	if omitted {
		separator := "\n"
		if options.Context != Prose {
			separator = " "
		}
		result = strings.TrimRight(result, " \n	") + separator + "[content truncated by terminal safety limits]"
	}
	return result
}

func runeDisplayWidth(value rune) int {
	if unicode.Is(unicode.Mn, value) || unicode.Is(unicode.Me, value) || value == 0xfe0f {
		return 0
	}
	if value >= 0x1100 && (value <= 0x115f || value == 0x2329 || value == 0x232a ||
		(value >= 0x2e80 && value <= 0xa4cf) || (value >= 0xac00 && value <= 0xd7a3) ||
		(value >= 0xf900 && value <= 0xfaff) || (value >= 0xfe10 && value <= 0xfe6f) ||
		(value >= 0xff00 && value <= 0xff60) || (value >= 0x1f300 && value <= 0x1faff)) {
		return 2
	}
	return 1
}

func consumeEscape(value string, index int) int {
	if index >= len(value) {
		return index
	}
	switch value[index] {
	case '[':
		return consumeCSI(value, index+1)
	case ']', 'P', '_', '^', 'X': // OSC, DCS, APC, PM, SOS
		return consumeStringControl(value, index+1)
	default:
		// Fe and two-byte escape forms.
		index++
		if index < len(value) && value[index-1] >= 0x20 && value[index-1] <= 0x2f {
			index++
		}
		return index
	}
}

func consumeCSI(value string, index int) int {
	for index < len(value) {
		byteValue := value[index]
		index++
		if byteValue >= 0x40 && byteValue <= 0x7e {
			break
		}
	}
	return index
}

func consumeStringControl(value string, index int) int {
	for index < len(value) {
		if value[index] == 0x07 || value[index] == 0x9c {
			return index + 1
		}
		if value[index] == 0x1b && index+1 < len(value) && value[index+1] == '\\' {
			return index + 2
		}
		index++
	}
	return index
}

func dangerousFormat(value rune) bool {
	switch value {
	case 0x061c, 0x200b, 0x200c, 0x200d, 0x200e, 0x200f,
		0x202a, 0x202b, 0x202c, 0x202d, 0x202e,
		0x2060, 0x2061, 0x2062, 0x2063, 0x2064,
		0x2066, 0x2067, 0x2068, 0x2069, 0xfeff:
		return true
	}
	return false
}

func binaryLike(value string) bool {
	if value == "" {
		return false
	}
	controls := 0
	for _, item := range value {
		if item == 0 || (unicode.IsControl(item) && item != '\n' && item != '\t') {
			controls++
		}
	}
	return controls > 4 && controls*20 > utf8.RuneCountInString(value)
}
