package command

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestAppendStartupLinePromisesAutomaticDelivery(t *testing.T) {
	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	queuedBytes := 0
	queued := appendStartupLine(cmd, nil, &queuedBytes, "hey", 1024)
	if len(queued) != 1 || queued[0] != "hey" || queuedBytes != 3 {
		t.Fatalf("queued input mismatch: queued=%q bytes=%d", queued, queuedBytes)
	}
	text := output.String()
	if !strings.Contains(text, "will run automatically when ready") || strings.Contains(text, "not sent") {
		t.Fatalf("misleading startup queue output: %q", text)
	}
}
