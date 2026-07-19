package tui

import (
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/term"
)

type Profile string

const (
	RichInteractive  Profile = "rich-interactive"
	PlainInteractive Profile = "accessible-plain-interactive"
	Machine          Profile = "machine-noninteractive"
)

type Capabilities struct {
	Profile         Profile
	StdinTTY        bool
	StdoutTTY       bool
	Width           int
	Height          int
	Color           bool
	Unicode         bool
	AlternateScreen bool
	BracketedPaste  bool
	ReducedMotion   bool
	Term            string
	OS              string
}

func Detect(input io.Reader, output io.Writer, lookup func(string) string) Capabilities {
	if lookup == nil {
		lookup = os.Getenv
	}
	capability := Capabilities{Width: 80, Height: 24, Unicode: true, OS: runtime.GOOS, Term: boundedEnv(lookup("TERM"), 128)}
	in, inOK := input.(*os.File)
	for {
		wrapped, ok := output.(interface{ Underlying() io.Writer })
		if !ok {
			break
		}
		output = wrapped.Underlying()
	}
	out, outOK := output.(*os.File)
	capability.StdinTTY = inOK && term.IsTerminal(int(in.Fd()))
	capability.StdoutTTY = outOK && term.IsTerminal(int(out.Fd()))
	if outOK {
		if width, height, err := term.GetSize(int(out.Fd())); err == nil && width > 0 && height > 0 {
			capability.Width, capability.Height = width, height
		}
	}
	if !capability.StdinTTY || !capability.StdoutTTY {
		capability.Profile = Machine
		return capability
	}
	plain := capability.Term == "dumb" || truthy(lookup("AEGIS_ACCESSIBLE")) || truthy(lookup("AEGIS_PLAIN"))
	capability.ReducedMotion = plain || truthy(lookup("AEGIS_REDUCED_MOTION"))
	capability.Color = lookup("NO_COLOR") == "" && capability.Term != "dumb"
	capability.Unicode = !truthy(lookup("AEGIS_ASCII")) && capability.Term != "dumb"
	capability.AlternateScreen = !plain && capability.Term != "dumb"
	capability.BracketedPaste = !plain && capability.Term != "dumb"
	if plain {
		capability.Profile = PlainInteractive
	} else {
		capability.Profile = RichInteractive
	}
	return capability
}

func boundedEnv(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) > maximum {
		value = value[:maximum]
	}
	return Sanitize(value, SanitizeOptions{Context: SecurityField, MaxBytes: maximum, MaxRunes: maximum, MaxLines: 1, MaxWidth: maximum})
}

func truthy(value string) bool {
	result, err := strconv.ParseBool(strings.TrimSpace(value))
	return err == nil && result
}
