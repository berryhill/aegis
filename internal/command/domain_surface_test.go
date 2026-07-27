package command

import (
	"io"
	"testing"
)

func TestRootDoesNotExposeLegacyPlumbingCommand(t *testing.T) {
	root := NewRoot(Dependencies{In: nil, Out: io.Discard, Err: io.Discard, Version: "test", IsTerminal: func(io.Reader, io.Writer) bool { return false }})
	for _, command := range root.Commands() {
		if command.Name() == "plumbing" {
			t.Fatal("legacy plumbing command remains on the production surface")
		}
	}
}
