package config

import (
	"os"
	"path/filepath"
	"testing"
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
