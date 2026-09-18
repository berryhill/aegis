package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"regexp"
)

// LocalInference is an immutable, credential-free route, not a provider secret.
// Model is selected by HermesConfig.Model; ModelDigest pins the installed artifact.
type LocalInference struct {
	Kind        string `json:"kind"`
	Endpoint    string `json:"endpoint"`
	ModelDigest string `json:"model_digest"`
}

var localModelDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func (r LocalInference) Validate() error {
	u, err := url.Parse(r.Endpoint)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.Host == "" {
		return errors.New("local inference requires a literal HTTP loopback origin")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || r.Kind != "ollama" || !localModelDigest.MatchString(r.ModelDigest) {
		return errors.New("local inference requires Ollama, literal loopback, and exact sha256 model digest")
	}
	return nil
}

// UnmarshalJSON rejects ambiguous/partial route objects before canonicalization.
func (r *LocalInference) UnmarshalJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("local inference must be an object")
	}
	values := map[string]string{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok {
			return errors.New("invalid local inference key")
		}
		if name != "kind" && name != "endpoint" && name != "model_digest" {
			return errors.New("unknown local inference field")
		}
		if _, exists := values[name]; exists {
			return errors.New("duplicate local inference field")
		}
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("null local inference field")
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		values[name] = value
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF || len(values) != 3 {
		return errors.New("incomplete local inference object")
	}
	candidate := LocalInference{Kind: values["kind"], Endpoint: values["endpoint"], ModelDigest: values["model_digest"]}
	if err := candidate.Validate(); err != nil {
		return err
	}
	*r = candidate
	return nil
}

// ValidateLocalInferenceAuthority is repeated at runtime boundaries even when
// the charter has previously passed validation. No tool or credential authority
// may accompany this deliberately bounded route.
func ValidateLocalInferenceAuthority(h HermesConfig, tools, credentials []string) error {
	if h.LocalInference == nil {
		if h.Provider == "ollama" {
			return errors.New("Ollama requires an immutable local inference binding")
		}
		return nil
	}
	if err := h.LocalInference.Validate(); err != nil {
		return err
	}
	if h.Provider != "ollama" || h.Model == "" || len(tools) != 0 || len(credentials) != 0 || len(h.Toolsets) != 0 || len(h.MCPServers) != 0 || len(h.Plugins) != 0 || h.Profile != "" || h.PersistentHome {
		return errors.New("local inference requires credential-free, tool-free disposable Ollama authority")
	}
	return nil
}
