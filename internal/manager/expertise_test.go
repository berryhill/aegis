package manager

import (
	"strings"
	"testing"
)

func TestPlatformExpertiseProjectionIsVersionedDigestBoundAndIdentitySafe(t *testing.T) {
	projection := PlatformExpertise()
	if projection.SchemaVersion != "aegis.manager.expertise.v1" || projection.Version != "aegis.platform.expertise.v2" {
		t.Fatalf("unversioned expertise projection: %+v", projection)
	}
	if !strings.HasPrefix(projection.Digest, "sha256:") || len(projection.Digest) != 71 {
		t.Fatalf("invalid expertise digest: %q", projection.Digest)
	}
	for _, required := range []string{
		"canonical built-in Aegis Agent",
		"separate from Hermes profile state",
		"disposable Aegis-owned HERMES_HOME",
		"no ambient profile, configuration, memory, skills, plugins, MCP, or credentials",
		"immutable, digest-bound revisions",
		"ownership and accountability",
		"exact enabled Agent revision",
		"exact Agent and Loop revisions",
		"typed submission",
		"durable rejection or exactly one durable queue item",
		"single-winner claim",
		"bounded attempt",
		"verification receipt",
		"distinct terminal disposition",
		"Credentials and capability declarations are separate from Agent registration",
		"model may propose",
		"never authorizes",
		"/agents readiness",
		"/agents list",
		"/agents show",
		"protected intake",
	} {
		if !strings.Contains(projection.Content, required) {
			t.Errorf("expertise projection missing %q", required)
		}
	}
	if err := projection.Validate(); err != nil {
		t.Fatalf("validate expertise: %v", err)
	}
	for _, required := range []string{projection.Version, projection.Digest, projection.Content} {
		if !strings.Contains(ManagerSystemInstruction(), required) {
			t.Errorf("manager instruction is not bound to expertise component %q", required)
		}
	}
}

func TestPlatformExpertiseValidationRejectsSubstitution(t *testing.T) {
	projection := PlatformExpertise()
	projection.Content += " substituted"
	if err := projection.Validate(); err == nil {
		t.Fatal("modified expertise content retained authority")
	}
}
