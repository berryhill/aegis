package core

import "errors"

const ControllerCodexAccessTokenV1 = "controller-codex-access-token.v1"

// ConfiguredProviderV1 records an approved model selection, NOT authentication.
// Runtime resolvers must deny this mode before consulting any credential source.
// Connecting it requires a separately approved charter successor.
const ConfiguredProviderV1 = "configured-provider.v1"

// ProviderAuthentication selects a closed controller transport policy, not Agent
// credential use. ConfiguredProviderV1 is explicitly non-executable metadata;
// ControllerCodexAccessTokenV1 approves transport but does not prove a connection.
// Omission retains the historical scope contract and canonical encoding. A material change requires a newly approved charter,
// mandate and clean session; old provider scopes are never reclassified.
type ProviderAuthentication struct {
	Mode string `json:"mode"`
}

func ValidateProviderAuthentication(h HermesConfig, tools, credentials []string) error {
	if err := validateSingleProviderAuthentication(h, tools, credentials); err != nil {
		return err
	}
	for _, lane := range h.AllowedLanes {
		if lane.Provider == h.Provider && lane.ProviderAuthentication == nil && lane.LocalInference == nil {
			lane.ProviderAuthentication = h.ProviderAuthentication
			lane.LocalInference = h.LocalInference
		}
		if err := validateSingleProviderAuthentication(h.WithLane(lane), tools, credentials); err != nil {
			return err
		}
	}
	return nil
}

func validateSingleProviderAuthentication(h HermesConfig, tools, credentials []string) error {
	if h.ProviderAuthentication == nil {
		return nil
	}
	switch h.ProviderAuthentication.Mode {
	case ConfiguredProviderV1:
		// This closed metadata set is independent of transport qualification.
		if h.Provider != "codex" && h.Provider != "openai-codex" && h.Provider != "antigravity-oauth" {
			return errors.New("unsupported configured provider")
		}
	case ControllerCodexAccessTokenV1:
		if h.Provider != "codex" && h.Provider != "openai-codex" {
			return errors.New("unsupported controller provider authentication")
		}
	default:
		return errors.New("unsupported controller provider authentication")
	}
	if h.Model == "" || h.Model == "none" || h.LocalInference != nil {
		return errors.New("unsupported controller provider authentication")
	}
	if len(tools) != 0 || len(h.Toolsets) != 0 || len(credentials) != 0 || len(h.MCPServers) != 0 || len(h.Plugins) != 0 || h.Profile != "" || h.PersistentHome {
		return errors.New("controller provider authentication requires zero Agent tool and credential authority")
	}
	return nil
}
