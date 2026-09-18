package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func localBinding() HermesConfig {
	return HermesConfig{Provider: "ollama", Model: "exact:1", LocalInference: &LocalInference{Kind: "ollama", Endpoint: "http://127.0.0.1:11434", ModelDigest: "sha256:" + strings.Repeat("a", 64)}}
}

func TestLocalInferenceCharterAndSealedAuthority(t *testing.T) {
	c := completePolicyCharter()
	c.Stanzas[0].Hermes = localBinding()
	c.Stanzas[0].Grant.Tools = nil
	c.Stanzas[0].Scopes.Credentials = nil
	if _, err := Canonicalize(c); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c)
	if _, err := DecodeCharter(strings.NewReader(string(raw))); err != nil {
		t.Fatal(err)
	}
	rawRoute, _ := json.Marshal(c.Stanzas[0].Hermes.LocalInference)
	invalid := strings.Replace(string(raw), string(rawRoute), "null", 1)
	if _, err := DecodeCharter(strings.NewReader(invalid)); err == nil {
		t.Fatal("accepted null route")
	}
	m, a := testAuthorityBinding()
	m.Hermes = localBinding()
	m.Tools = nil
	m.Scopes.Credentials = nil
	a.Authority.Hermes = localBinding()
	a.Authority.Tools = nil
	a.Authority.Credentials = nil
	a.Digest = AuthorityContextDigest(a)
	if err := ValidateAuthorityContext(a, m); err != nil {
		t.Fatal(err)
	}
	a.Authority.Hermes.LocalInference.Endpoint = "http://127.0.0.1:11435"
	if ValidateAuthorityContext(a, m) == nil {
		t.Fatal("accepted stale digest")
	}
	a.Digest = AuthorityContextDigest(a)
	if ValidateAuthorityContext(a, m) == nil {
		t.Fatal("accepted route differing from mandate")
	}
}

func TestLocalInferenceAuthority(t *testing.T) {
	h := localBinding()
	if err := ValidateLocalInferenceAuthority(h, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"http://localhost:11434", "https://127.0.0.1:11434", "http://127.0.0.1:11434/", "http://127.0.0.1:11434?", "http://127.0.0.1:11434?q=x", "http://user@127.0.0.1:11434", "http://192.168.0.1:11434", "http://127.0.0.1:11434#x"} {
		candidate := localBinding()
		candidate.LocalInference.Endpoint = endpoint
		if ValidateLocalInferenceAuthority(candidate, nil, nil) == nil {
			t.Errorf("accepted endpoint %q", endpoint)
		}
	}
	for _, mutate := range []func(*HermesConfig){
		func(h *HermesConfig) { h.Provider = "openrouter" },
		func(h *HermesConfig) { h.LocalInference = nil },
		func(h *HermesConfig) { h.LocalInference.Kind = "other" },
		func(h *HermesConfig) { h.LocalInference.ModelDigest = "sha256:bad" },
		func(h *HermesConfig) { h.Toolsets = []string{"no_mcp"} },
		func(h *HermesConfig) { h.MCPServers = []string{"server"} },
		func(h *HermesConfig) { h.Plugins = []string{"plugin"} },
		func(h *HermesConfig) { h.PersistentHome = true },
		func(h *HermesConfig) { h.Profile = "ambient" },
	} {
		candidate := localBinding()
		mutate(&candidate)
		if ValidateLocalInferenceAuthority(candidate, nil, nil) == nil {
			t.Fatal("accepted widened local route")
		}
	}
	if ValidateLocalInferenceAuthority(h, []string{"web"}, nil) == nil || ValidateLocalInferenceAuthority(h, nil, []string{"provider:ollama"}) == nil {
		t.Fatal("accepted tool or credential authority")
	}
}

func TestLocalInferenceStrictDecode(t *testing.T) {
	valid := `{"kind":"ollama","endpoint":"http://127.0.0.1:11434","model_digest":"sha256:` + strings.Repeat("a", 64) + `"}`
	for _, raw := range []string{"null", `{}`, strings.Replace(valid, `"kind":"ollama"`, `"kind":"ollama","kind":"ollama"`, 1), strings.Replace(valid, `"kind":"ollama"`, `"kind":null`, 1), strings.Replace(valid, `"kind":"ollama"`, `"kind":"ollama","extra":"x"`, 1)} {
		var route LocalInference
		if json.Unmarshal([]byte(raw), &route) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	var route LocalInference
	if err := json.Unmarshal([]byte(valid), &route); err != nil {
		t.Fatal(err)
	}
}

func TestLocalInferenceDigestBindingAndAbsentCompatibility(t *testing.T) {
	h := HermesConfig{Model: "old", Provider: "none"}
	encoded, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	const legacy = `{"profile":"","persistent_home":false,"mcp_servers":null,"plugins":null,"toolsets":null,"model":"old","provider":"none"}`
	if string(encoded) != legacy {
		t.Fatalf("legacy bytes changed: %s", encoded)
	}
	h = localBinding()
	before := Digest(h)
	h.LocalInference.Endpoint = "http://127.0.0.1:11435"
	if before == Digest(h) {
		t.Fatal("endpoint absent from digest")
	}
	before = Digest(h)
	h.LocalInference.ModelDigest = "sha256:" + strings.Repeat("b", 64)
	if before == Digest(h) {
		t.Fatal("model digest absent from digest")
	}
}
