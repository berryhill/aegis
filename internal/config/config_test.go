package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadStrictAndEnvironmentPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aegis.yaml")
	data := []byte("state_dir: /from-file\nruntime_default: hermes\nhermes_executable: hermes\nprincipal:\n  id: principal-1\n  name: Principal Operator\n  uid: '4242'\n  user: operator\n  auth_ttl: 5m\napi:\n  listen: 127.0.0.1:8443\n  read_timeout: 15s\n  write_timeout: 30s\n  shutdown_timeout: 10s\n  max_body_bytes: 1048576\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_STATE_DIR", "/from-env")
	c, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.StateDir != "/from-env" {
		t.Fatalf("state_dir=%q", c.StateDir)
	}
}
func TestExampleConfigurationRemainsLoadable(t *testing.T) {
	example, err := os.ReadFile(filepath.Join("..", "..", "examples", "aegis.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	document := strings.ReplaceAll(string(example), "REPLACE_WITH_LOCAL_UID", "4242")
	document = strings.ReplaceAll(document, "REPLACE_WITH_LOCAL_USERNAME", "operator")
	path := filepath.Join(t.TempDir(), "aegis.yaml")
	if err = os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path, nil); err != nil {
		t.Fatalf("examples/aegis.yaml is not a valid launch asset: %v", err)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	data := []byte("unknown_security_switch: true\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, nil); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestLoadPreservesValidEnvironmentOnlyConfiguration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AEGIS_PRINCIPAL_UID", "4242")
	t.Setenv("AEGIS_PRINCIPAL_USER", "operator")
	configuration, err := Load("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if configuration.Principal.UID != "4242" || configuration.Principal.User != "operator" {
		t.Fatalf("environment principal not preserved: %#v", configuration.Principal)
	}
}

func TestLiteralArgisDefaultsIgnoreXDGScattering(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "elsewhere-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "elsewhere-state"))
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	defaults := Defaults()
	if path != filepath.Join(home, ".argis", "aegis.yaml") || defaults.StateDir != filepath.Join(home, ".argis", "state") || defaults.Audit.CheckpointDir != filepath.Join(home, ".argis", "state", "audit-checkpoints") || defaults.Principal.AuthTTL != 15*time.Minute || defaults.Manager.Hermes.TurnTimeout != 5*time.Minute || defaults.Manager.Inference.RequestTimeout != 5*time.Minute {
		t.Fatalf("scattered defaults: path=%q state=%q checkpoints=%q", path, defaults.StateDir, defaults.Audit.CheckpointDir)
	}
}

func TestDefaultInspectionDetectsLegacyAndCanonicalAmbiguity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	legacy := filepath.Join(home, ".config", "aegis", "aegis.yaml")
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("malformed: [\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Inspect(""); got.State != StateLegacy || got.ReasonCode != "legacy-layout-detected" {
		t.Fatalf("legacy=%+v", got)
	}
	if err := os.Mkdir(filepath.Join(home, ".argis"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".argis", "unknown"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Inspect(""); got.State != StateAmbiguous || got.ReasonCode != "canonical_and_legacy_layout_ambiguous" {
		t.Fatalf("ambiguity=%+v", got)
	}
}

func TestInspectRejectsPartialEnvironmentConfigurationAsAmbiguous(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AEGIS_PRINCIPAL_UID", "4242")
	inspection := Inspect("")
	if inspection.State != StateAmbiguous || inspection.ReasonCode != "configuration_environment_ambiguous" {
		t.Fatalf("inspection=%+v", inspection)
	}
}

func TestCredentialBindingsFailClosed(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
	}{
		{"unsupported type", func(c *Config) {
			c.Credentials.ProviderAuth["test"] = CredentialBinding{Type: "file", SourceEnv: "TEST_KEY", TargetEnv: "TEST_KEY"}
		}},
		{"reserved target", func(c *Config) {
			c.Credentials.ProviderAuth["test"] = CredentialBinding{Type: "environment", SourceEnv: "TEST_KEY", TargetEnv: "HERMES_HOME"}
		}},
		{"invalid source", func(c *Config) {
			c.Credentials.ProviderAuth["test"] = CredentialBinding{Type: "environment", SourceEnv: "lowercase", TargetEnv: "TEST_KEY"}
		}},
		{"missing design provider", func(c *Config) { c.Credentials.DesignProvider = "missing" }},
		{"incomplete TLS identity", func(c *Config) { c.API.TLSCertFile = "server.crt" }},
		{"TLS on Unix socket", func(c *Config) {
			c.API.UnixSocket, c.API.TLSCertFile, c.API.TLSKeyFile = "aegis.sock", "server.crt", "server.key"
		}},
		{"incomplete credential authority", func(c *Config) {
			c.Credentials.Authority = CredentialAuthority{Database: "authority.db", Custody: "systemd", KEKCredential: "aegis-kek"}
		}},
		{"mixed credential custody", func(c *Config) {
			c.Credentials.Authority = CredentialAuthority{Database: "authority.db", DeploymentID: "node-1", Custody: "systemd", KEKCredential: "aegis-kek", KEKFile: "kek.json"}
		}},
		{"systemd credential traversal", func(c *Config) {
			c.Credentials.Authority = CredentialAuthority{Database: "authority.db", DeploymentID: "node-1", Custody: "systemd", KEKCredential: "../aegis-kek"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Defaults()
			c.Principal = Principal{ID: "principal", Name: "Principal", UID: "1000", User: "operator", AuthTTL: c.Principal.AuthTTL}
			tt.edit(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("unsafe credential configuration accepted")
			}
		})
	}
}

func TestCredentialAuthorityCustodyModesValidate(t *testing.T) {
	for _, authority := range []CredentialAuthority{
		{Database: "authority.db", DeploymentID: "node-1", Custody: "systemd", KEKCredential: "aegis-kek"},
		{Database: "authority.db", DeploymentID: "node-1", Custody: "host-file", KEKFile: "aegis-kek.json"},
	} {
		configuration := Defaults()
		configuration.Principal = Principal{ID: "principal", Name: "Principal", UID: "1000", User: "operator", AuthTTL: configuration.Principal.AuthTTL}
		configuration.Credentials.Authority = authority
		if err := configuration.Validate(); err != nil {
			t.Fatalf("valid authority configuration rejected: %v", err)
		}
	}
}

func TestRedactedHidesCredentialKeyPath(t *testing.T) {
	configuration := Defaults()
	configuration.Credentials.Authority.KEKFile = "/private/aegis-kek.json"
	redacted := Redacted(configuration)
	if redacted.Credentials.Authority.KEKFile != "[REDACTED]" {
		t.Fatal("credential key path was not redacted")
	}
}

func TestManagerConfigurationFailsClosed(t *testing.T) {
	tests := []func(*Config){
		func(c *Config) { c.Manager.Runtime = "other" },
		func(c *Config) { c.Manager.SecurityContext = "principal" },
		func(c *Config) { c.Manager.Hermes.ContextLength = 4096 },
		func(c *Config) { c.Manager.Inference.Runtime = "cloud" },
		func(c *Config) {
			c.Manager.Inference.Mode = "managed"
			c.Manager.Inference.Endpoint = "http://127.0.0.1:11434"
		},
		func(c *Config) { c.Manager.Inference.KeepAlive = -time.Second },
		func(c *Config) { c.Manager.Inference.Model = "mutable:latest" },
		func(c *Config) { c.Manager.Inference.Model = "exact:1"; c.Manager.Inference.ModelDigest = "sha256:bad" },
		func(c *Config) { c.Manager.Ingress.BoundedDecodeDepth = 100 },
		func(c *Config) { c.Manager.Transcript.Retention = "forever" },
	}
	for index, edit := range tests {
		configuration := Defaults()
		configuration.Principal = Principal{ID: "principal", Name: "Principal", UID: "1000", User: "operator", AuthTTL: configuration.Principal.AuthTTL}
		edit(&configuration)
		if err := configuration.Validate(); err == nil {
			t.Fatalf("unsafe manager case %d accepted", index)
		}
	}
	configuration := Defaults()
	configuration.Principal = Principal{ID: "principal", Name: "Principal", UID: "1000", User: "operator", AuthTTL: configuration.Principal.AuthTTL}
	configuration.Manager.Inference.Model = "exact:1"
	configuration.Manager.Inference.ModelDigest = "sha256:" + strings.Repeat("a", 64)
	configuration.Manager.Inference.Certification = filepath.Join(configuration.StateDir, "manager", "exact-1.certification.json")
	if err := configuration.Validate(); err != nil {
		t.Fatalf("valid pinned manager config rejected: %v", err)
	}
}
