package consoleweb

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRegistryCardSummariesRetainAdmissionBoundary(t *testing.T) {
	for _, tc := range []struct{ full, short string }{
		{"Lifecycle eligible; fresh authority admission required", "Fresh admission required"},
		{"Execution denied until a new enabled revision", "Denied until enabled"},
		{"Terminal; no later revisions permitted", "Terminal"},
		{"Unknown evidence; deny", "Unknown evidence; deny"},
	} {
		if got := registryCardReadiness(tc.full); got != tc.short {
			t.Errorf("summary %q = %q; want %q", tc.full, got, tc.short)
		}
	}
	record := RecordModel{Key: "office", Label: "office", Lifecycle: "enabled", Readiness: "Lifecycle eligible; fresh authority admission required", Authority: "2 capabilities · 0 policies declared", Provisioning: "Not asserted by Registry record"}
	var rendered bytes.Buffer
	if err := AgentCard(SurfaceModel{}, record).Render(context.Background(), &rendered); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`title="Lifecycle eligible; fresh authority admission required">Fresh admission required</dd>`,
		`<dt>Declared authority</dt>`,
		`title="Not asserted by Registry record">Not asserted</dd>`,
	} {
		if !strings.Contains(rendered.String(), want) {
			t.Errorf("missing compact non-authorizing card evidence: %s", want)
		}
	}
	if got := registryCardProvisioning("Receipt unavailable"); got != "Receipt unavailable" {
		t.Fatalf("unknown provisioning evidence rewritten: %q", got)
	}
}
