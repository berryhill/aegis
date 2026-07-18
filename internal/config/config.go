package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	StateDir         string      `mapstructure:"state_dir" json:"state_dir"`
	RuntimeDefault   string      `mapstructure:"runtime_default" json:"runtime_default"`
	HermesExecutable string      `mapstructure:"hermes_executable" json:"hermes_executable"`
	Principal        Principal   `mapstructure:"principal" json:"principal"`
	API              API         `mapstructure:"api" json:"api"`
	Retention        Retention   `mapstructure:"retention" json:"retention"`
	Audit            Audit       `mapstructure:"audit" json:"audit"`
	Credentials      Credentials `mapstructure:"credentials" json:"credentials"`
	Manager          Manager     `mapstructure:"manager" json:"manager"`
}

type Manager struct {
	Enabled         bool              `mapstructure:"enabled" json:"enabled"`
	Runtime         string            `mapstructure:"runtime" json:"runtime"`
	SecurityContext string            `mapstructure:"security_context" json:"security_context"`
	Hermes          ManagerHermes     `mapstructure:"hermes" json:"hermes"`
	Inference       ManagerInference  `mapstructure:"inference" json:"inference"`
	Ingress         ManagerIngress    `mapstructure:"ingress" json:"ingress"`
	Transcript      ManagerTranscript `mapstructure:"transcript" json:"transcript"`
}
type ManagerHermes struct {
	ContextLength        int           `mapstructure:"context_length" json:"context_length"`
	GatewayStartTimeout  time.Duration `mapstructure:"gateway_start_timeout" json:"gateway_start_timeout"`
	TurnTimeout          time.Duration `mapstructure:"turn_timeout" json:"turn_timeout"`
	MaximumResponseBytes int64         `mapstructure:"maximum_response_bytes" json:"maximum_response_bytes"`
}
type ManagerInference struct {
	Runtime              string        `mapstructure:"runtime" json:"runtime"`
	Mode                 string        `mapstructure:"mode" json:"mode"`
	Executable           string        `mapstructure:"executable" json:"executable"`
	Endpoint             string        `mapstructure:"endpoint" json:"endpoint,omitempty"`
	Model                string        `mapstructure:"model" json:"model,omitempty"`
	ModelDigest          string        `mapstructure:"model_digest" json:"model_digest,omitempty"`
	KeepAlive            time.Duration `mapstructure:"keep_alive" json:"keep_alive"`
	StartTimeout         time.Duration `mapstructure:"start_timeout" json:"start_timeout"`
	RequestTimeout       time.Duration `mapstructure:"request_timeout" json:"request_timeout"`
	MaximumRequestBytes  int64         `mapstructure:"maximum_request_bytes" json:"maximum_request_bytes"`
	MaximumResponseBytes int64         `mapstructure:"maximum_response_bytes" json:"maximum_response_bytes"`
}
type ManagerIngress struct {
	MaximumMessageBytes int64         `mapstructure:"maximum_message_bytes" json:"maximum_message_bytes"`
	MaximumMessageRunes int           `mapstructure:"maximum_message_runes" json:"maximum_message_runes"`
	ScanTimeout         time.Duration `mapstructure:"scan_timeout" json:"scan_timeout"`
	BoundedDecodeDepth  int           `mapstructure:"bounded_decode_depth" json:"bounded_decode_depth"`
}
type ManagerTranscript struct {
	Retention string `mapstructure:"retention" json:"retention"`
}
type Principal struct {
	ID      string        `mapstructure:"id" json:"id"`
	Name    string        `mapstructure:"name" json:"name"`
	UID     string        `mapstructure:"uid" json:"uid"`
	User    string        `mapstructure:"user" json:"user"`
	AuthTTL time.Duration `mapstructure:"auth_ttl" json:"auth_ttl"`
}
type API struct {
	Listen          string        `mapstructure:"listen" json:"listen"`
	UnixSocket      string        `mapstructure:"unix_socket" json:"unix_socket,omitempty"`
	Token           string        `mapstructure:"token" json:"token"`
	TLSCertFile     string        `mapstructure:"tls_cert_file" json:"tls_cert_file,omitempty"`
	TLSKeyFile      string        `mapstructure:"tls_key_file" json:"tls_key_file,omitempty"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout" json:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout" json:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout" json:"shutdown_timeout"`
	MaxBodyBytes    int64         `mapstructure:"max_body_bytes" json:"max_body_bytes"`
}
type Retention struct {
	DesignHomes  bool `mapstructure:"design_homes" json:"design_homes"`
	SessionHomes bool `mapstructure:"session_homes" json:"session_homes"`
}
type Audit struct {
	CheckpointDir string `mapstructure:"checkpoint_dir" json:"checkpoint_dir"`
}
type CredentialBinding struct {
	Type      string `mapstructure:"type" json:"type"`
	SourceEnv string `mapstructure:"source_env" json:"source_env"`
	TargetEnv string `mapstructure:"target_env" json:"target_env"`
}
type Credentials struct {
	References     map[string]CredentialBinding `mapstructure:"references" json:"references"`
	ProviderAuth   map[string]CredentialBinding `mapstructure:"provider_auth" json:"provider_auth"`
	DesignProvider string                       `mapstructure:"design_provider" json:"design_provider,omitempty"`
	Authority      CredentialAuthority          `mapstructure:"authority" json:"authority"`
}

type CredentialAuthority struct {
	Database      string           `mapstructure:"database" json:"database,omitempty"`
	DeploymentID  string           `mapstructure:"deployment_id" json:"deployment_id,omitempty"`
	Custody       string           `mapstructure:"custody" json:"custody,omitempty"`
	KEKCredential string           `mapstructure:"kek_credential" json:"kek_credential,omitempty"`
	KEKFile       string           `mapstructure:"kek_file" json:"kek_file,omitempty"`
	Broker        CredentialBroker `mapstructure:"broker" json:"broker,omitempty"`
}

type CredentialBroker struct {
	Socket        string                       `mapstructure:"socket" json:"socket,omitempty"`
	AllowedUID    uint32                       `mapstructure:"allowed_uid" json:"allowed_uid,omitempty"`
	AllowedGID    uint32                       `mapstructure:"allowed_gid" json:"allowed_gid,omitempty"`
	CapabilityTTL time.Duration                `mapstructure:"capability_ttl" json:"capability_ttl,omitempty"`
	MaxBodyBytes  int64                        `mapstructure:"max_body_bytes" json:"max_body_bytes,omitempty"`
	Timeout       time.Duration                `mapstructure:"timeout" json:"timeout,omitempty"`
	Destinations  map[string]BrokerDestination `mapstructure:"destinations" json:"destinations,omitempty"`
}

type BrokerDestination struct {
	URL          string   `mapstructure:"url" json:"url"`
	Repositories []string `mapstructure:"repositories" json:"repositories"`
}

func (c Credentials) MarshalJSON() ([]byte, error) {
	type credentialOutput struct {
		References     map[string]CredentialBinding `json:"references"`
		ProviderAuth   map[string]CredentialBinding `json:"provider_auth"`
		DesignProvider string                       `json:"design_provider,omitempty"`
		Authority      *CredentialAuthority         `json:"authority,omitempty"`
	}
	var authority *CredentialAuthority
	if c.Authority.Database != "" || c.Authority.DeploymentID != "" || c.Authority.Custody != "" || c.Authority.KEKCredential != "" || c.Authority.KEKFile != "" || c.Authority.Broker.Socket != "" || len(c.Authority.Broker.Destinations) != 0 {
		copy := c.Authority
		authority = &copy
	}
	return json.Marshal(credentialOutput{References: c.References, ProviderAuth: c.ProviderAuth, DesignProvider: c.DesignProvider, Authority: authority})
}

func validEnvironmentName(name string) bool {
	if name == "" || !(name[0] == '_' || name[0] >= 'A' && name[0] <= 'Z') {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !(name[i] == '_' || name[i] >= 'A' && name[i] <= 'Z' || name[i] >= '0' && name[i] <= '9') {
			return false
		}
	}
	return true
}

func Defaults() Config {
	h, _ := os.UserHomeDir()
	return Config{StateDir: filepath.Join(h, ".local", "state", "aegis"), RuntimeDefault: "hermes", HermesExecutable: "hermes", Principal: Principal{ID: "principal", Name: "Principal", AuthTTL: 5 * time.Minute}, API: API{Listen: "127.0.0.1:8443", ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, ShutdownTimeout: 10 * time.Second, MaxBodyBytes: 1 << 20}, Audit: Audit{CheckpointDir: filepath.Join(h, ".local", "state", "aegis-checkpoints")}, Credentials: Credentials{References: map[string]CredentialBinding{}, ProviderAuth: map[string]CredentialBinding{}}, Manager: Manager{Enabled: true, Runtime: "hermes", SecurityContext: "secrets-manager", Hermes: ManagerHermes{ContextLength: 65536, GatewayStartTimeout: 20 * time.Second, TurnTimeout: 120 * time.Second, MaximumResponseBytes: 1 << 20}, Inference: ManagerInference{Runtime: "ollama", Mode: "managed", Executable: "ollama", KeepAlive: 5 * time.Minute, StartTimeout: 30 * time.Second, RequestTimeout: 120 * time.Second, MaximumRequestBytes: 4 << 20, MaximumResponseBytes: 4 << 20}, Ingress: ManagerIngress{MaximumMessageBytes: 256 << 10, MaximumMessageRunes: 256 << 10, ScanTimeout: 250 * time.Millisecond, BoundedDecodeDepth: 2}, Transcript: ManagerTranscript{Retention: "session"}}}
}
func (c Config) Validate() error {
	var es []error
	if c.StateDir == "" {
		es = append(es, errors.New("state_dir is required"))
	}
	if c.RuntimeDefault != "hermes" {
		es = append(es, errors.New("runtime_default must be hermes"))
	}
	if c.HermesExecutable == "" {
		es = append(es, errors.New("hermes_executable is required"))
	}
	if strings.TrimSpace(c.Principal.ID) == "" || strings.TrimSpace(c.Principal.Name) == "" || strings.TrimSpace(c.Principal.UID) == "" || strings.TrimSpace(c.Principal.User) == "" {
		es = append(es, errors.New("principal must explicitly define id, name, uid, and user"))
	}
	if c.Principal.AuthTTL <= 0 || c.Principal.AuthTTL > 15*time.Minute {
		es = append(es, errors.New("principal.auth_ttl must be positive and at most 15m"))
	}
	if c.API.Listen == "" || c.API.ReadTimeout <= 0 || c.API.WriteTimeout <= 0 || c.API.ShutdownTimeout <= 0 || c.API.MaxBodyBytes < 1024 {
		es = append(es, errors.New("API limits and timeouts must be explicit and positive"))
	}
	if (c.API.TLSCertFile == "") != (c.API.TLSKeyFile == "") {
		es = append(es, errors.New("api.tls_cert_file and api.tls_key_file must be configured together"))
	}
	if c.API.UnixSocket != "" && c.API.TLSCertFile != "" {
		es = append(es, errors.New("API TLS is only supported for TCP listeners"))
	}
	if c.Audit.CheckpointDir == "" {
		es = append(es, errors.New("audit.checkpoint_dir is required"))
	}
	manager := c.Manager
	if manager.Runtime != "hermes" || manager.SecurityContext != "secrets-manager" || manager.Hermes.ContextLength < 64000 || manager.Hermes.ContextLength > 1<<20 || manager.Hermes.GatewayStartTimeout <= 0 || manager.Hermes.GatewayStartTimeout > time.Minute || manager.Hermes.TurnTimeout <= 0 || manager.Hermes.TurnTimeout > 10*time.Minute || manager.Hermes.MaximumResponseBytes < 1024 || manager.Hermes.MaximumResponseBytes > 16<<20 {
		es = append(es, errors.New("manager Hermes configuration is invalid or outside supported bounds"))
	}
	if manager.Inference.Runtime != "ollama" || (manager.Inference.Mode != "managed" && manager.Inference.Mode != "external-local") || manager.Inference.Executable == "" || manager.Inference.KeepAlive <= 0 || manager.Inference.KeepAlive > 30*time.Minute || manager.Inference.StartTimeout <= 0 || manager.Inference.StartTimeout > 2*time.Minute || manager.Inference.RequestTimeout <= 0 || manager.Inference.RequestTimeout > 10*time.Minute || manager.Inference.MaximumRequestBytes < 1024 || manager.Inference.MaximumRequestBytes > 16<<20 || manager.Inference.MaximumResponseBytes < 1024 || manager.Inference.MaximumResponseBytes > 16<<20 {
		es = append(es, errors.New("manager Ollama configuration is invalid or outside supported bounds"))
	}
	if manager.Inference.Mode == "managed" && manager.Inference.Endpoint != "" {
		es = append(es, errors.New("managed Ollama mode forbids a configured endpoint"))
	}
	if (manager.Inference.Model == "") != (manager.Inference.ModelDigest == "") || (manager.Inference.ModelDigest != "" && (!strings.HasPrefix(manager.Inference.ModelDigest, "sha256:") || len(manager.Inference.ModelDigest) != 71)) {
		es = append(es, errors.New("manager model name and exact sha256 digest must be configured together"))
	}
	if manager.Ingress.MaximumMessageBytes < 1024 || manager.Ingress.MaximumMessageBytes > 4<<20 || manager.Ingress.MaximumMessageRunes < 1024 || manager.Ingress.MaximumMessageRunes > 4<<20 || manager.Ingress.ScanTimeout <= 0 || manager.Ingress.ScanTimeout > time.Second || manager.Ingress.BoundedDecodeDepth < 0 || manager.Ingress.BoundedDecodeDepth > 3 || manager.Transcript.Retention != "session" {
		es = append(es, errors.New("manager ingress or transcript configuration is invalid"))
	}
	validateBinding := func(name string, binding CredentialBinding) {
		reserved := map[string]bool{"PATH": true, "HOME": true, "HERMES_HOME": true, "HERMES_PYTHON_SRC_ROOT": true, "HERMES_TUI_TOOLSETS": true, "HERMES_TUI_SKILLS": true, "LD_PRELOAD": true, "PYTHONPATH": true}
		if binding.Type != "environment" || !validEnvironmentName(binding.SourceEnv) || !validEnvironmentName(binding.TargetEnv) || reserved[binding.TargetEnv] {
			es = append(es, fmt.Errorf("credential %q must use type environment with source_env and target_env", name))
		}
	}
	for name, binding := range c.Credentials.References {
		validateBinding(name, binding)
	}
	for provider, binding := range c.Credentials.ProviderAuth {
		validateBinding("provider_auth."+provider, binding)
	}
	if c.Credentials.DesignProvider != "" {
		if _, ok := c.Credentials.ProviderAuth[c.Credentials.DesignProvider]; !ok {
			es = append(es, fmt.Errorf("credentials.design_provider %q has no provider_auth binding", c.Credentials.DesignProvider))
		}
	}
	authority := c.Credentials.Authority
	if authority.Database != "" || authority.DeploymentID != "" || authority.Custody != "" || authority.KEKCredential != "" || authority.KEKFile != "" {
		if authority.Database == "" || authority.DeploymentID == "" {
			es = append(es, errors.New("credentials.authority.database and deployment_id are required when credential authority is configured"))
		}
		switch authority.Custody {
		case "systemd":
			if authority.KEKCredential == "" || authority.KEKCredential == "." || authority.KEKCredential == ".." || authority.KEKFile != "" || filepath.IsAbs(authority.KEKCredential) || filepath.Base(authority.KEKCredential) != authority.KEKCredential {
				es = append(es, errors.New("systemd credential custody requires one simple kek_credential name and no kek_file"))
			}
		case "host-file":
			if authority.KEKFile == "" || authority.KEKCredential != "" {
				es = append(es, errors.New("host-file credential custody requires kek_file and no kek_credential"))
			}
		default:
			es = append(es, errors.New("credentials.authority.custody must be systemd or host-file"))
		}
		broker := authority.Broker
		if broker.Socket != "" {
			if !filepath.IsAbs(broker.Socket) || strings.HasPrefix(broker.Socket, "@") || broker.CapabilityTTL <= 0 || broker.CapabilityTTL > 15*time.Minute || broker.MaxBodyBytes < 256 || broker.MaxBodyBytes > 1<<20 || broker.Timeout <= 0 || broker.Timeout > 30*time.Second || len(broker.Destinations) != 1 {
				es = append(es, errors.New("credential broker requires an absolute pathname socket, bounded positive limits, and exactly one github-api destination"))
			}
			for id, destination := range broker.Destinations {
				if id != "github-api" || strings.TrimSpace(destination.URL) == "" || len(destination.Repositories) == 0 {
					es = append(es, errors.New("credential broker requires the github-api URL and at least one exact repository"))
				}
				for _, repository := range destination.Repositories {
					parts := strings.Split(repository, "/")
					if len(parts) != 2 || strings.TrimSpace(parts[0]) != parts[0] || strings.TrimSpace(parts[1]) != parts[1] || parts[0] == "" || parts[1] == "" || strings.Contains(repository, "..") {
						es = append(es, errors.New("credential broker repositories must be exact owner/repository identifiers"))
					}
				}
			}
		}
	}
	return errors.Join(es...)
}

// Load implements: flags > environment > config file > defaults. It creates an isolated Viper instance and returns an immutable typed snapshot.
func Load(path string, flags *pflag.FlagSet) (Config, error) {
	v := viper.New()
	d := Defaults()
	v.SetDefault("state_dir", d.StateDir)
	v.SetDefault("runtime_default", d.RuntimeDefault)
	v.SetDefault("hermes_executable", d.HermesExecutable)
	v.SetDefault("principal.id", d.Principal.ID)
	v.SetDefault("principal.name", d.Principal.Name)
	v.SetDefault("principal.auth_ttl", d.Principal.AuthTTL)
	v.SetDefault("api.listen", d.API.Listen)
	v.SetDefault("api.read_timeout", d.API.ReadTimeout)
	v.SetDefault("api.write_timeout", d.API.WriteTimeout)
	v.SetDefault("api.shutdown_timeout", d.API.ShutdownTimeout)
	v.SetDefault("api.max_body_bytes", d.API.MaxBodyBytes)
	v.SetDefault("audit.checkpoint_dir", d.Audit.CheckpointDir)
	v.SetDefault("manager", d.Manager)
	v.SetEnvPrefix("AEGIS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	for _, k := range []string{"state_dir", "runtime_default", "hermes_executable", "principal.id", "principal.name", "principal.uid", "principal.user", "principal.auth_ttl", "api.listen", "api.unix_socket", "api.token", "api.tls_cert_file", "api.tls_key_file", "api.read_timeout", "api.write_timeout", "api.shutdown_timeout", "api.max_body_bytes", "retention.design_homes", "retention.session_homes", "audit.checkpoint_dir", "manager.enabled", "manager.inference.mode", "manager.inference.executable", "manager.inference.endpoint", "manager.inference.model", "manager.inference.model_digest"} {
		_ = v.BindEnv(k)
	}
	if flags != nil {
		if err := v.BindPFlags(flags); err != nil {
			return Config{}, err
		}
	}
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read config: %w", err)
		}
	}
	var c Config
	if err := v.UnmarshalExact(&c); err != nil {
		return Config{}, fmt.Errorf("strict config decode: %w", err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return c, nil
}
func Redacted(c Config) Config {
	if c.API.Token != "" {
		c.API.Token = "[REDACTED]"
	}
	if c.Credentials.Authority.KEKFile != "" {
		c.Credentials.Authority.KEKFile = "[REDACTED]"
	}
	return c
}
