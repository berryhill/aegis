package skillbundle

import (
	"encoding/json"
	"os"
	"testing"
)

func TestManifestMinimumHermesCompatibility(t *testing.T) {
	data, err := os.ReadFile("../../skills/aegis-skills.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Bundle.HermesRange = ">=0.18.0"
	if err := validateManifestShape(manifest); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", ">=0.17.0", ">=0.18.0,<0.19.0", "*"} {
		manifest.Bundle.HermesRange = invalid
		if err := validateManifestShape(manifest); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
}
