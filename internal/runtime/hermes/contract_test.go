package hermes

import (
	"strings"
	"testing"
)

func TestParseVersionOutputAcceptsQualifiedHermes(t *testing.T) {
	installed, err := ParseVersionOutput([]byte("Hermes Agent v0.18.27\nInstall directory: /opt/hermes\n"))
	if err != nil {
		t.Fatalf("qualified Hermes output rejected: %v", err)
	}
	if installed.Version != "0.18.27" || installed.Installation != "/opt/hermes" {
		t.Fatalf("unexpected parsed installation: %#v", installed)
	}
}

func TestParseVersionOutputAcceptsDecoratedHermesVersion(t *testing.T) {
	output := "Hermes Agent v0.18.2 (2026.7.7.2) · upstream 81413f00 · local 594308d4 (+1 carried commit)\n" +
		"Install directory: /home/operator/.hermes/hermes-agent\n" +
		"Install method: git\n" +
		"Python: 3.11.15\n" +
		"OpenAI SDK: 2.24.0\n"
	installed, err := ParseVersionOutput([]byte(output))
	if err != nil {
		t.Fatalf("decorated Hermes output rejected: %v", err)
	}
	if installed.Version != "0.18.2" || installed.Installation != "/home/operator/.hermes/hermes-agent" {
		t.Fatalf("unexpected parsed installation: %#v", installed)
	}
}

func TestParseVersionOutputIdentifiesUnsupportedHermesVersion(t *testing.T) {
	_, err := ParseVersionOutput([]byte("Hermes Agent v0.14.0\n"))
	if err == nil {
		t.Fatal("unsupported Hermes version accepted")
	}
	want := "unsupported Hermes version 0.14.0: adapter supports >=0.18.0,<0.19.0"
	if err.Error() != want {
		t.Fatalf("unexpected unsupported-version error: got %q, want %q", err, want)
	}
}

func TestParseVersionOutputFailsClosed(t *testing.T) {
	tests := map[string]string{
		"empty":                  "",
		"unsupported version":    "Hermes Agent v0.19.0\n",
		"prerelease":             "Hermes Agent v0.18.2-rc.1\n",
		"leading zero":           "Hermes Agent v0.18.02\n",
		"duplicate version":      "Hermes Agent v0.18.1\nHermes Agent v0.18.2\n",
		"malformed then valid":   "Hermes Agent version unknown\nHermes Agent v0.18.2\n",
		"malformed decoration":   "Hermes Agent v0.18.2 (release) · upstream unknown · local unknown\n",
		"duplicate installation": "Hermes Agent v0.18.2\nInstall directory: /one\nInstall directory: /two\n",
		"relative installation":  "Hermes Agent v0.18.2\nInstall directory: relative\n",
		"nul":                    "Hermes Agent v0.18.2\x00\n",
	}
	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseVersionOutput([]byte(output)); err == nil {
				t.Fatal("unsupported or ambiguous Hermes output accepted")
			}
		})
	}
	oversized := []byte("Hermes Agent v0.18.2\n" + strings.Repeat("x", maximumVersionOutput))
	if _, err := ParseVersionOutput(oversized); err == nil {
		t.Fatal("oversized Hermes output accepted")
	}
}

func TestValidateCapabilitiesRequiresExactAssumptions(t *testing.T) {
	capabilities := append([]string(nil), requiredCapabilities...)
	if err := ValidateCapabilities(capabilities); err != nil {
		t.Fatalf("required capabilities rejected: %v", err)
	}
	for i, capability := range requiredCapabilities {
		t.Run(capability, func(t *testing.T) {
			missing := append([]string(nil), requiredCapabilities[:i]...)
			missing = append(missing, requiredCapabilities[i+1:]...)
			if err := ValidateCapabilities(missing); err == nil {
				t.Fatal("missing required capability accepted")
			}
		})
	}
	if err := ValidateCapabilities(append(capabilities, capabilities[0])); err == nil {
		t.Fatal("ambiguous duplicate capability accepted")
	}
}
