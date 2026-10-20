package app

import (
	"strings"
	"testing"
)

func TestControllerCodexProbeIdentifierIsStoreSafe(t *testing.T) {
	s := testService(t)
	id := controllerProviderProbeID("principal-1", "format-probe-1")
	if strings.Contains(id, ":") || id != controllerProviderProbeID("principal-1", "format-probe-1") || id == controllerProviderProbeID("principal-1", "format-probe-2") {
		t.Fatal("probe identity is invalid, unstable or conflicting")
	}
	if err := s.Store.Create("controller-provider-probes", id, map[string]string{"status": "fixture-only"}); err != nil {
		t.Fatal("generated probe identity rejected by owning store", err)
	}
}
