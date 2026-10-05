package core

import "errors"

const ControllerCodexAccessTokenV1 = "controller-codex-access-token.v1"

// ProviderAuthentication explicitly approves controller-owned inference transport,
// not Agent credential use. Omission retains the historical scope contract and
// canonical encoding. A material change requires a newly approved charter,
// mandate and clean session; old provider scopes are never reclassified.
type ProviderAuthentication struct {
	Mode string `json:"mode"`
}

func ValidateProviderAuthentication(h HermesConfig, tools, credentials []string) error {
	if h.ProviderAuthentication == nil {
		return nil
	}
	if h.ProviderAuthentication.Mode != ControllerCodexAccessTokenV1 || (h.Provider != "codex" && h.Provider != "openai-codex") || h.Model == "" || h.Model == "none" || h.LocalInference != nil {
		return errors.New("unsupported controller provider authentication")
	}
	if len(tools) != 0 || len(h.Toolsets) != 0 || len(credentials) != 0 || len(h.MCPServers) != 0 || len(h.Plugins) != 0 || h.Profile != "" || h.PersistentHome {
		return errors.New("controller provider authentication requires zero Agent tool and credential authority")
	}
	return nil
}
