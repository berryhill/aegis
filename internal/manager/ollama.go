package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const SupportedOllamaConstraint = ">=0.18.0,<1.0.0"

type OllamaClient struct {
	Endpoint string
	Timeout  time.Duration
}

type OllamaModel struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	ModifiedAt string `json:"modified_at"`
	Size       int64  `json:"size"`
	Digest     string `json:"digest"`
	Details    struct {
		Family            string   `json:"family"`
		Families          []string `json:"families"`
		ParentModel       string   `json:"parent_model"`
		Format            string   `json:"format"`
		ParameterSize     string   `json:"parameter_size"`
		QuantizationLevel string   `json:"quantization_level"`
		ContextLength     int      `json:"context_length"`
		EmbeddingLength   int      `json:"embedding_length"`
	} `json:"details"`
	Capabilities []string `json:"capabilities"`
}

type PullProgress struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
}

func NewOllamaClient(endpoint string, timeout time.Duration) (*OllamaClient, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || !loopbackHost(parsed.Hostname()) || timeout <= 0 || timeout > 5*time.Minute {
		return nil, errors.New("external Ollama endpoint must be an HTTP loopback origin")
	}
	return &OllamaClient{Endpoint: strings.TrimSuffix(endpoint, "/"), Timeout: timeout}, nil
}

func (c *OllamaClient) request(ctx context.Context, method, path string, body []byte, target any) error {
	request, err := http.NewRequestWithContext(ctx, method, c.Endpoint+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return errors.New("Ollama redirect denied")
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Ollama returned status %d", response.StatusCode)
	}
	if target == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(data) > 4<<20 {
		return errors.New("Ollama response invalid or oversized")
	}
	return strictDecode(data, target)
}

func (c *OllamaClient) Version(ctx context.Context) (string, error) {
	var result struct {
		Version string `json:"version"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/version", nil, &result); err != nil {
		return "", err
	}
	if result.Version == "" {
		return "", errors.New("Ollama version missing")
	}
	if !supportedOllamaVersion(result.Version) {
		return "", fmt.Errorf("unsupported Ollama version %q; required %s", result.Version, SupportedOllamaConstraint)
	}
	return result.Version, nil
}

func supportedOllamaVersion(value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	numbers := [3]int{}
	for index, part := range parts {
		numeric := strings.SplitN(part, "-", 2)[0]
		parsed, err := strconv.Atoi(numeric)
		if err != nil || parsed < 0 {
			return false
		}
		numbers[index] = parsed
	}
	return numbers[0] == 0 && numbers[1] >= 18
}

func (c *OllamaClient) VerifyModel(ctx context.Context, name, digest string) (OllamaModel, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return OllamaModel{}, err
	}
	for _, model := range models {
		if model.Name == name || model.Model == name {
			resolved := model.Digest
			if !strings.HasPrefix(resolved, "sha256:") {
				resolved = "sha256:" + resolved
			}
			if resolved != digest {
				return OllamaModel{}, errors.New(ReasonDigestMismatch)
			}
			return model, nil
		}
	}
	return OllamaModel{}, errors.New(ReasonModelAbsent)
}

func (c *OllamaClient) ListModels(ctx context.Context) ([]OllamaModel, error) {
	var result struct {
		Models []OllamaModel `json:"models"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/tags", nil, &result); err != nil {
		return nil, err
	}
	if len(result.Models) > 10000 {
		return nil, errors.New("Ollama model inventory is oversized")
	}
	return result.Models, nil
}

// Pull performs the network mutation permitted only by an explicit onboarding
// plan. The mutable registry name is not identity; callers must rediscover and
// bind the exact resulting digest after completion.
func (c *OllamaClient) Pull(ctx context.Context, model string, progress func(PullProgress)) error {
	if model == "" || len(model) > 256 {
		return errors.New("Ollama pull model is invalid")
	}
	body, err := json.Marshal(map[string]any{"model": model, "stream": true})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	// Pulls routinely exceed the ordinary inference request timeout. The caller's
	// context is the explicit cancellation/deadline authority for this mutation.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("Ollama pull returned status %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 16<<20))
	succeeded := false
	for count := 0; ; count++ {
		if count > 100000 {
			return errors.New("Ollama pull progress exceeds event limit")
		}
		var event PullProgress
		if err = decoder.Decode(&event); errors.Is(err, io.EOF) {
			if !succeeded {
				return errors.New("Ollama pull ended without a success event")
			}
			return nil
		}
		if err != nil {
			return errors.New("Ollama pull progress is malformed")
		}
		if len(event.Status) > 512 || len(event.Digest) > 128 || event.Total < 0 || event.Completed < 0 || (event.Total > 0 && event.Completed > event.Total) {
			return errors.New("Ollama pull progress is invalid")
		}
		if progress != nil {
			progress(event)
		}
		if event.Status == "success" {
			succeeded = true
		}
	}
}

func NormalizeModelDigest(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "sha256:") {
		value = "sha256:" + value
	}
	if len(value) != 71 {
		return "", errors.New("Ollama artifact digest is not an exact sha256 identity")
	}
	for _, character := range value[len("sha256:"):] {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return "", errors.New("Ollama artifact digest is not an exact lowercase sha256 identity")
		}
	}
	return value, nil
}

func (c *OllamaClient) Load(ctx context.Context, model string, contextLength int, keepAlive time.Duration) error {
	if model == "" || contextLength < 64000 || keepAlive <= 0 || keepAlive > 30*time.Minute {
		return errors.New("invalid model load policy")
	}
	body, _ := json.Marshal(map[string]any{"model": model, "keep_alive": keepAlive.String(), "options": map[string]int{"num_ctx": contextLength}})
	return c.request(ctx, http.MethodPost, "/api/generate", body, nil)
}

func (c *OllamaClient) Unload(ctx context.Context, model string) error {
	if model == "" {
		return errors.New("model is required")
	}
	body, _ := json.Marshal(map[string]any{"model": model, "keep_alive": 0})
	return c.request(ctx, http.MethodPost, "/api/generate", body, nil)
}

func parseVersion(value string) ([3]int, error) {
	var result [3]int
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return result, errors.New("invalid semantic version")
	}
	_, err := fmt.Sscanf(strings.Join(parts, "."), "%d.%d.%d", &result[0], &result[1], &result[2])
	return result, err
}
