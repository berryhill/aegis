package buildinfo

import "testing"

func TestNormalize(t *testing.T) {
	if got := Normalize("v1.2.3"); got != "1.2.3" {
		t.Fatalf("Normalize() = %q", got)
	}
}

func TestStableModuleVersion(t *testing.T) {
	for _, version := range []string{"v0.1.0", "v1.2.3", "v10.20.30"} {
		if !isStable(version) {
			t.Errorf("isStable(%q) = false", version)
		}
	}
	for _, version := range []string{"dev", "(devel)", "v1.2", "v1.2.3-beta.1", "v0.0.0-20260717235235-deadbeef", "v1.02.3"} {
		if isStable(version) {
			t.Errorf("isStable(%q) = true", version)
		}
	}
}
