package skillbundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Explicit maintenance opt-in uses the same canonical functions as validation.
func TestRefreshManifest(t *testing.T) {
	if os.Getenv("AEGIS_REFRESH_SKILL_MANIFEST") != "1" {
		t.Skip("maintenance opt-in required")
	}
	root := "../.."
	name := filepath.Join(root, ManifestName)
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for i := range m.Skills {
		s := &m.Skills[i]
		for j := range s.Files {
			f := &s.Files[j]
			b, e := os.ReadFile(filepath.Join(root, s.Path, f.Path))
			if e != nil {
				t.Fatal(e)
			}
			f.Size = int64(len(b))
			f.SHA256 = sha256Digest(b)
		}
		s.ContentDigest = fileSetDigest(s.Files)
	}
	m.Bundle.ContentDigest = bundleDigest(m.Skills)
	data, err = json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = Validate(root); err != nil {
		t.Fatal(err)
	}
}
