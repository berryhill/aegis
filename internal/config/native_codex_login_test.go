package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeCodexLoginBinding(t *testing.T) {
	c := Defaults()
	c.Principal.UID = "1000"
	c.Principal.User = "operator"
	c.Credentials.ControllerCodexLogin = &ControllerCodexLogin{ProfileHome: "/home/operator/.hermes/profiles/xander", AccountID: "synthetic-account", CredentialID: "nc-" + strings.Repeat("a", 64), ApprovedBy: c.Principal.ID}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "aegis.yaml")
	// Strict decode remains authoritative; a typo cannot silently disable binding.
	invalid := strings.Replace(string(data), `"account_id":`, `"unknown_native_field":`, 1)
	if err = os.WriteFile(file, []byte(invalid), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(file, nil); err == nil || !strings.Contains(err.Error(), "unknown_native_field") {
		t.Fatal("unknown native config field was not rejected by strict decoder", err)
	}
	shared := c
	sharedBinding := *c.Credentials.ControllerCodexLogin
	sharedBinding.ProfileHome = "/home/operator/.hermes"
	shared.Credentials.ControllerCodexLogin = &sharedBinding
	if err := shared.Validate(); err != nil {
		t.Fatal("explicit shared source schema rejected", err)
	}
	for _, change := range []func(*Config){
		func(c *Config) { c.Credentials.ControllerCodexLogin.AccountID = "" },
		func(c *Config) { c.Credentials.ControllerCodexLogin.CredentialID = "raw-native-id" },
		func(c *Config) { c.Credentials.ControllerCodexLogin.CredentialID = "nc-" + strings.Repeat("a", 65) },
		func(c *Config) { c.Credentials.ControllerCodexLogin.ProfileHome = "relative" },
		func(c *Config) { c.Credentials.ControllerCodexLogin.ProfileHome = "/home/operator/.hermes/unknown" },
		func(c *Config) { c.Credentials.ControllerCodexLogin.ApprovedBy = "other" },
		func(c *Config) {
			c.Credentials.ProviderAuth["codex"] = EnvironmentCredentialBinding{Type: "environment", SourceEnv: "SOURCE", TargetEnv: "AEGIS_CODEX_ACCESS_TOKEN"}
		},
		func(c *Config) { c.Credentials.DesignProvider = "codex" },
	} {
		copy := c
		binding := *c.Credentials.ControllerCodexLogin
		copy.Credentials.ControllerCodexLogin = &binding
		copy.Credentials.ProviderAuth = map[string]EnvironmentCredentialBinding{}
		change(&copy)
		if copy.Validate() == nil {
			t.Fatal("unsafe native binding admitted")
		}
	}
}
