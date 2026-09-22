package hermes

import "testing"

func TestInstalledHermesVersionOutput(t *testing.T) {
	output := "Hermes Agent v0.21.3 (2026.9.14) · upstream e589b739 · local 56631482 (+19448 carried commits)\nInstall directory: /home/silas/.hermes/hermes-agent\n"
	installed, err := ParseVersionOutput([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != "0.21.3" {
		t.Fatalf("version=%q", installed.Version)
	}
}

func TestMinimumHermesVersion(t *testing.T) {
	for _, version := range []string{"0.18.0", "0.18.999", "0.19.0", "0.21.3", "0.100.0", "1.0.0", "999999999999999999999999999.0.0"} {
		t.Run(version, func(t *testing.T) {
			if !SupportedVersion(version) {
				t.Errorf("minimum-compatible version %q rejected", version)
			}
			if _, err := ParseVersionOutput([]byte("Hermes Agent v" + version + "\n")); err != nil {
				t.Error(err)
			}
		})
	}
	for _, version := range []string{"", "0.0.0", "0.17.999", "0.18", "v0.18.0", "0.018.0", "01.0.0", "0.18.00", "0.18.0-rc.1", "0.21.3+build", "-1.0.0", "0.21.3\n"} {
		t.Run("deny-"+version, func(t *testing.T) {
			if SupportedVersion(version) {
				t.Errorf("invalid or old version %q accepted", version)
			}
		})
	}
}
