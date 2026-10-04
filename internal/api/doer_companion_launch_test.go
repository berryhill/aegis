package api

import (
	"context"
	"strings"
	"testing"
)

func TestDoerCompanionLauncherRejectsBearerBeforeAnyProcess(t *testing.T) {
	for _, bearer := range []string{"", "has space", strings.Repeat("x", 4097), "newline\n"} {
		bound := false
		status := launchDoerCompanion(context.Background(), "/not-a-socket", "http://127.0.0.1", "pending", bearer, func(int) { bound = true })
		if status != "transport_unavailable" || bound {
			t.Fatal("invalid transport reached subprocess")
		}
	}
}
func TestDoerCompanionOutputBound(t *testing.T) {
	out := &boundedCompanionOutput{}
	if _, err := out.Write([]byte(strings.Repeat("x", 4096))); err != nil {
		t.Fatal(err)
	}
	if _, err := out.Write([]byte("x")); err == nil || out.Len() != 4096 {
		t.Fatal("unbounded companion diagnostics")
	}
}
