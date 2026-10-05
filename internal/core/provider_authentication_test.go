package core

import (
	"encoding/json"
	"testing"
)

func TestControllerProviderAuthenticationRequiresExplicitNewAuthority(t *testing.T) {
	h := HermesConfig{Provider: "codex", Model: "gpt-6.1-sol", ProviderAuthentication: &ProviderAuthentication{Mode: ControllerCodexAccessTokenV1}}
	if err := ValidateProviderAuthentication(h, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*HermesConfig){
		func(h *HermesConfig) { h.Provider = "openai" },
		func(h *HermesConfig) { h.ProviderAuthentication.Mode = "unknown" },
		func(h *HermesConfig) { h.LocalInference = &LocalInference{} },
	} {
		copy := h
		a := *h.ProviderAuthentication
		copy.ProviderAuthentication = &a
		mutate(&copy)
		if ValidateProviderAuthentication(copy, nil, nil) == nil {
			t.Fatal("invalid provider authentication accepted")
		}
	}
	if ValidateProviderAuthentication(h, []string{"terminal"}, nil) == nil || ValidateProviderAuthentication(h, nil, []string{"provider:codex"}) == nil {
		t.Fatal("controller transport widened agent authority")
	}
	old := HermesConfig{Provider: "codex", Model: "gpt-6.1-sol"}
	if ValidateProviderAuthentication(old, nil, nil) != nil {
		t.Fatal("legacy helper must not reinterpret absent field")
	}
	data, _ := json.Marshal(old)
	var decoded map[string]any
	_ = json.Unmarshal(data, &decoded)
	if _, ok := decoded["provider_authentication"]; ok {
		t.Fatal("legacy canonical bytes changed")
	}
}
