package manager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"go.yaml.in/yaml/v3"
)

type InstalledCandidate struct {
	Candidate Candidate   `json:"candidate"`
	Artifact  OllamaModel `json:"artifact"`
	Digest    string      `json:"digest"`
	Endpoint  string      `json:"endpoint"`
}

type ModelDiscovery struct {
	Mode       string               `json:"mode"`
	Endpoint   string               `json:"endpoint"`
	Boundary   string               `json:"boundary"`
	Version    string               `json:"ollama_version"`
	Installed  []InstalledCandidate `json:"installed_candidates"`
	Missing    []Candidate          `json:"missing_candidates"`
	NoDownload bool                 `json:"no_download"`
	NextStep   string               `json:"next_step"`
}

func CandidateByID(id string) (Candidate, error) {
	for _, candidate := range Candidates() {
		if candidate.ID == id {
			return candidate, nil
		}
	}
	return Candidate{}, errors.New("candidate is not in the official traceable manager registry")
}

func DiscoverInstalledCandidates(ctx context.Context, endpoint string, timeout time.Duration) (ModelDiscovery, error) {
	client, err := NewOllamaClient(endpoint, timeout)
	if err != nil {
		return ModelDiscovery{}, err
	}
	version, err := client.Version(ctx)
	if err != nil {
		return ModelDiscovery{}, fmt.Errorf("%s: %w", ReasonOllamaUnavailable, err)
	}
	models, err := client.ListModels(ctx)
	if err != nil {
		return ModelDiscovery{}, fmt.Errorf("%s: %w", ReasonOllamaUnavailable, err)
	}
	result := ModelDiscovery{Mode: "external-local", Endpoint: client.Endpoint, Boundary: "operator-managed loopback daemon; Aegis does not own daemon startup, updates, or shutdown", Version: version, NoDownload: true}
	for _, candidate := range Candidates() {
		found := false
		for _, model := range models {
			if model.Name != candidate.OllamaName && model.Model != candidate.OllamaName {
				continue
			}
			digest, digestErr := NormalizeModelDigest(model.Digest)
			if digestErr != nil {
				return ModelDiscovery{}, digestErr
			}
			result.Installed = append(result.Installed, InstalledCandidate{Candidate: candidate, Artifact: model, Digest: digest, Endpoint: client.Endpoint})
			found = true
		}
		if !found {
			result.Missing = append(result.Missing, candidate)
		}
	}
	if len(result.Installed) == 0 {
		result.NextStep = "No approved candidate is visible at this exact route. Install separately only with explicit operator authorization, then rerun discovery."
	} else {
		result.NextStep = "Preview one installed candidate with: aegis manager model configure CANDIDATE_ID --endpoint " + client.Endpoint
	}
	return result, nil
}

type ModelConfigPreview struct {
	ConfigPath           string                       `json:"config_path"`
	Mode                 string                       `json:"mode"`
	Endpoint             string                       `json:"endpoint"`
	CandidateID          string                       `json:"candidate_id"`
	Model                string                       `json:"model"`
	Digest               string                       `json:"model_digest"`
	Certification        string                       `json:"certification"`
	DaemonOwnership      string                       `json:"daemon_ownership"`
	ModelStoreVisibility string                       `json:"model_store_visibility"`
	NoDownload           bool                         `json:"no_download"`
	NoCopy               bool                         `json:"no_copy"`
	OriginalDigest       string                       `json:"original_config_digest"`
	ResultDigest         string                       `json:"result_config_digest"`
	Changes              map[string]ModelConfigChange `json:"changes"`
	document             []byte
}

type ModelConfigChange struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after"`
}

func PreviewExternalModelConfiguration(configPath, stateDir, certificationPath string, installed InstalledCandidate) (ModelConfigPreview, error) {
	inspection := config.Inspect(configPath)
	if inspection.State != config.StateValid || !inspection.FilePresent {
		return ModelConfigPreview{}, errors.New("model configuration requires one securely owned valid existing config file")
	}
	if err := secureConfigDirectory(inspection.Path); err != nil {
		return ModelConfigPreview{}, err
	}
	registered, err := CandidateByID(installed.Candidate.ID)
	if err != nil || (registered.OllamaName != installed.Artifact.Name && registered.OllamaName != installed.Artifact.Model) {
		return ModelConfigPreview{}, errors.New("installed artifact does not exactly match the selected approved candidate")
	}
	digest, err := NormalizeModelDigest(installed.Digest)
	if err != nil {
		return ModelConfigPreview{}, err
	}
	state, err := filepath.Abs(stateDir)
	if err != nil {
		return ModelConfigPreview{}, err
	}
	certification := certificationPath
	if certification == "" {
		certification = filepath.Join(stateDir, "manager", "certifications", registered.ID+".json")
	}
	certification, err = filepath.Abs(certification)
	if err != nil {
		return ModelConfigPreview{}, err
	}
	relativeCertification, err := filepath.Rel(state, certification)
	if err != nil || relativeCertification == "." || relativeCertification == ".." || strings.HasPrefix(relativeCertification, ".."+string(filepath.Separator)) {
		return ModelConfigPreview{}, errors.New("certification destination must be below Aegis state")
	}
	original, err := os.ReadFile(inspection.Path)
	if err != nil {
		return ModelConfigPreview{}, err
	}
	var document yaml.Node
	if err = yaml.Unmarshal(original, &document); err != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return ModelConfigPreview{}, errors.New("existing configuration YAML cannot be safely updated")
	}
	root := document.Content[0]
	manager := mappingChild(root, "manager")
	configuredManager := inspection.Config.Manager
	setMappingScalar(manager, "enabled", strconv.FormatBool(configuredManager.Enabled))
	setMappingScalar(manager, "runtime", configuredManager.Runtime)
	setMappingScalar(manager, "security_context", configuredManager.SecurityContext)
	setMappingScalar(manager, "cleanup_timeout", configuredManager.CleanupTimeout.String())
	hermes := mappingChild(manager, "hermes")
	setMappingScalar(hermes, "context_length", strconv.Itoa(configuredManager.Hermes.ContextLength))
	setMappingScalar(hermes, "gateway_start_timeout", configuredManager.Hermes.GatewayStartTimeout.String())
	setMappingScalar(hermes, "turn_timeout", configuredManager.Hermes.TurnTimeout.String())
	setMappingScalar(hermes, "maximum_response_bytes", strconv.FormatInt(configuredManager.Hermes.MaximumResponseBytes, 10))
	inference := mappingChild(manager, "inference")
	setMappingScalar(inference, "runtime", "ollama")
	setMappingScalar(inference, "executable", configuredManager.Inference.Executable)
	setMappingScalar(inference, "keep_alive", configuredManager.Inference.KeepAlive.String())
	setMappingScalar(inference, "start_timeout", configuredManager.Inference.StartTimeout.String())
	setMappingScalar(inference, "request_timeout", configuredManager.Inference.RequestTimeout.String())
	setMappingScalar(inference, "maximum_request_bytes", strconv.FormatInt(configuredManager.Inference.MaximumRequestBytes, 10))
	setMappingScalar(inference, "maximum_response_bytes", strconv.FormatInt(configuredManager.Inference.MaximumResponseBytes, 10))
	setMappingScalar(inference, "mode", "external-local")
	setMappingScalar(inference, "endpoint", installedEndpoint(installed))
	setMappingScalar(inference, "model", registered.OllamaName)
	setMappingScalar(inference, "model_digest", digest)
	setMappingScalar(inference, "certification", certification)
	ingress := mappingChild(manager, "ingress")
	setMappingScalar(ingress, "maximum_message_bytes", strconv.FormatInt(configuredManager.Ingress.MaximumMessageBytes, 10))
	setMappingScalar(ingress, "maximum_message_runes", strconv.Itoa(configuredManager.Ingress.MaximumMessageRunes))
	setMappingScalar(ingress, "scan_timeout", configuredManager.Ingress.ScanTimeout.String())
	setMappingScalar(ingress, "bounded_decode_depth", strconv.Itoa(configuredManager.Ingress.BoundedDecodeDepth))
	transcript := mappingChild(manager, "transcript")
	setMappingScalar(transcript, "retention", configuredManager.Transcript.Retention)
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err = encoder.Encode(&document); err != nil {
		return ModelConfigPreview{}, err
	}
	_ = encoder.Close()
	originalSum := sha256.Sum256(original)
	resultSum := sha256.Sum256(encoded.Bytes())
	changes := map[string]ModelConfigChange{
		"manager.inference.mode":          {Before: configuredManager.Inference.Mode, After: "external-local"},
		"manager.inference.endpoint":      {Before: configuredManager.Inference.Endpoint, After: installedEndpoint(installed)},
		"manager.inference.model":         {Before: configuredManager.Inference.Model, After: registered.OllamaName},
		"manager.inference.model_digest":  {Before: configuredManager.Inference.ModelDigest, After: digest},
		"manager.inference.certification": {Before: configuredManager.Inference.Certification, After: certification},
	}
	return ModelConfigPreview{ConfigPath: inspection.Path, Mode: "external-local", Endpoint: installedEndpoint(installed), CandidateID: registered.ID, Model: registered.OllamaName, Digest: digest, Certification: certification, DaemonOwnership: "external operator; Aegis will not stop the daemon", ModelStoreVisibility: "the exact artifact is read in place; no import or copy into Aegis's private managed store", NoDownload: true, NoCopy: true, OriginalDigest: "sha256:" + hex.EncodeToString(originalSum[:]), ResultDigest: "sha256:" + hex.EncodeToString(resultSum[:]), Changes: changes, document: append([]byte(nil), encoded.Bytes()...)}, nil
}

func installedEndpoint(installed InstalledCandidate) string {
	// Discovery binds the endpoint separately; this field is populated by the
	// command before preview so an artifact cannot select its own route.
	return installed.Endpoint
}

func ApplyModelConfiguration(preview ModelConfigPreview) error {
	if len(preview.document) == 0 || preview.ConfigPath == "" || preview.OriginalDigest == "" {
		return errors.New("model configuration preview is incomplete")
	}
	current, err := os.ReadFile(preview.ConfigPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(current)
	if "sha256:"+hex.EncodeToString(sum[:]) != preview.OriginalDigest {
		return errors.New("configuration changed after preview; no write performed")
	}
	directory := filepath.Dir(preview.ConfigPath)
	temporary, err := os.CreateTemp(directory, ".aegis-model-config-*.yaml")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0600); err != nil {
		return err
	}
	if _, err = temporary.Write(preview.document); err != nil {
		return err
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if _, err = config.Load(temporaryPath, nil); err != nil {
		return fmt.Errorf("generated manager configuration failed strict validation: %w", err)
	}
	if err = os.Rename(temporaryPath, preview.ConfigPath); err != nil {
		return err
	}
	committed = true
	directoryHandle, openErr := os.Open(directory)
	if openErr != nil {
		return openErr
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}

func secureConfigDirectory(path string) error {
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return errors.New("configuration directory must be owned by the operator and not writable by group or others")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return errors.New("configuration directory is not owned by the current effective UID")
	}
	return nil
}

func mappingChild(parent *yaml.Node, key string) *yaml.Node {
	for index := 0; index+1 < len(parent.Content); index += 2 {
		if parent.Content[index].Value == key {
			if parent.Content[index+1].Kind != yaml.MappingNode {
				parent.Content[index+1] = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			return parent.Content[index+1]
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, keyNode, valueNode)
	return valueNode
}

func setMappingScalar(parent *yaml.Node, key, value string) {
	for index := 0; index+1 < len(parent.Content); index += 2 {
		if parent.Content[index].Value == key {
			parent.Content[index+1].Kind = yaml.ScalarNode
			parent.Content[index+1].Tag = "!!str"
			parent.Content[index+1].Value = value
			return
		}
	}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}
