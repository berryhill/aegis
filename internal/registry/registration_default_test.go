package registry

import (
	"context"
	"strings"
	"testing"
)

func TestRegistrationLifecycleDefaultsOnlyWhenOmitted(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        Lifecycle
		denied      bool
	}{
		{"omitted", "", LifecycleEnabled, false},
		{"enabled", `"lifecycle":"enabled",`, LifecycleEnabled, false},
		{"disabled", `"lifecycle":"disabled",`, LifecycleDisabled, false},
		{"retired", `"lifecycle":"retired",`, LifecycleRetired, false},
		{"null", `"lifecycle":null,`, "", true},
		{"empty", `"lifecycle":"",`, "", true},
		{"unknown", `"lifecycle":"active",`, "", true},
		{"type", `"lifecycle":true,`, "", true},
		{"duplicate", `"lifecycle":"disabled","lifecycle":"enabled",`, "", true},
		{"unknown field", `"surprise":true,`, "", true},
		{"case alias", `"Lifecycle":"disabled",`, "", true},
		{"case duplicate", `"lifecycle":"disabled","Lifecycle":"enabled",`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := strings.ReplaceAll(strings.ReplaceAll(testFleetFixture(), `"lifecycle":"disabled",`, `"lifecycle":"enabled",`), `"lifecycle":"enabled",`, tc.field)
			source, err := NewCurrentFleetFixtureSource([]byte(wire))
			if tc.denied {
				if err == nil {
					t.Fatal("malformed registration accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			candidates, err := source.Discover(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, candidate := range candidates {
				if candidate.Lifecycle != tc.want {
					t.Fatalf("lifecycle=%q want %q", candidate.Lifecycle, tc.want)
				}
			}
		})
	}
	revision := candidateRevision(testCandidate("agent-alpha", "fleet-agent-1"), 1)
	revision.Lifecycle = ""
	if _, err := SealRevision(revision); err == nil {
		t.Fatal("canonical revision must not default missing lifecycle")
	}
}
