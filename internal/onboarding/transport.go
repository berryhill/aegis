package onboarding

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"syscall"

	"github.com/berryhill/aegis/internal/config"
	"go.yaml.in/yaml/v3"
)

const TransportConfirmation = "APPLY SERVE TRANSPORT"

// TransportPlan is safe to render: reusable bearer material is intentionally
// private and never appears in JSON, previews, unit text, argv, or environment.
type TransportPlan struct {
	ConfigPath     string `json:"config_path"`
	TokenPath      string `json:"token_file"`
	UnixSocket     string `json:"unix_socket"`
	OriginalDigest string `json:"original_config_digest"`
	ResultDigest   string `json:"result_config_digest"`
	Confirmation   string `json:"confirmation"`
	PrincipalUID   string `json:"principal_uid"`
	PrincipalUser  string `json:"principal_user"`
	document       []byte
	token          []byte
}

// PreviewTransport admits only the supported token/socket-absent upgrade state.
// Existing complete transport is returned as an explicit no-op error so callers
// cannot accidentally rotate authentication material during onboarding.
func PreviewTransport(configPath string) (TransportPlan, error) {
	inspection := config.Inspect(configPath)
	if inspection.State != config.StateValid || !inspection.FilePresent {
		return TransportPlan{}, errors.New("serve transport reconciliation requires one secure file-backed valid configuration")
	}
	cfg := inspection.Config
	if cfg.API.Token != "" || cfg.API.TokenFile != "" || cfg.API.UnixSocket != "" {
		if cfg.API.Token != "" && cfg.API.UnixSocket != "" {
			return TransportPlan{}, errors.New("serve transport is already configured")
		}
		return TransportPlan{}, errors.New("partial or ambiguous serve transport requires operator repair")
	}
	current, err := user.Current()
	if err != nil || current.Uid != cfg.Principal.UID || current.Username != cfg.Principal.User {
		return TransportPlan{}, errors.New("configured principal does not match freshly authenticated host identity")
	}
	original, err := os.ReadFile(inspection.Path)
	if err != nil {
		return TransportPlan{}, err
	}
	var document yaml.Node
	if err = yaml.Unmarshal(original, &document); err != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return TransportPlan{}, errors.New("configuration YAML cannot be safely reconciled")
	}
	tokenRandom := make([]byte, 32)
	if _, err = rand.Read(tokenRandom); err != nil {
		return TransportPlan{}, err
	}
	token := make([]byte, hex.EncodedLen(len(tokenRandom))+1)
	hex.Encode(token, tokenRandom)
	token[len(token)-1] = '\n'
	tokenPath := filepath.Join(cfg.StateDir, "transport", "api.token")
	socket := filepath.Join(cfg.StateDir, "transport", "aegis.sock")
	api := mapChild(document.Content[0], "api")
	setScalar(api, "token_file", tokenPath)
	setScalar(api, "unix_socket", socket)
	removeScalar(api, "token")
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err = encoder.Encode(&document); err != nil {
		return TransportPlan{}, err
	}
	_ = encoder.Close()
	originalDigest := sha256.Sum256(original)
	resultDigest := sha256.Sum256(encoded.Bytes())
	return TransportPlan{
		ConfigPath: inspection.Path, TokenPath: tokenPath, UnixSocket: socket,
		OriginalDigest: hex.EncodeToString(originalDigest[:]), ResultDigest: hex.EncodeToString(resultDigest[:]),
		Confirmation: TransportConfirmation, PrincipalUID: current.Uid, PrincipalUser: current.Username,
		document: encoded.Bytes(), token: token,
	}, nil
}

// ApplyTransport reauthenticates and revalidates the exact preview before an
// atomic replacement. A newly published token is removed if publication fails.
func ApplyTransport(ctx context.Context, plan TransportPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := user.Current()
	if err != nil || current.Uid != plan.PrincipalUID || current.Username != plan.PrincipalUser {
		return errors.New("authenticated principal changed after serve transport preview")
	}
	before, err := os.ReadFile(plan.ConfigPath)
	if err != nil || digest(before) != plan.OriginalDigest {
		return errors.New("configuration changed after serve transport preview")
	}
	if err = ensureOwnedDirectory(filepath.Dir(plan.TokenPath)); err != nil {
		return err
	}
	created := false
	if _, err = os.Lstat(plan.TokenPath); errors.Is(err, os.ErrNotExist) {
		created = true
		if err = writeExclusive(plan.TokenPath, plan.token); err != nil {
			return fmt.Errorf("publish protected transport token: %w", err)
		}
	} else if err == nil {
		return errors.New("transport token path appeared after preview")
	} else {
		return err
	}
	committed := false
	defer func() {
		if created && !committed {
			_ = os.Remove(plan.TokenPath)
		}
	}()
	if latest, readErr := os.ReadFile(plan.ConfigPath); readErr != nil || digest(latest) != plan.OriginalDigest {
		return errors.New("configuration changed during serve transport reconciliation")
	}
	if digest(plan.document) != plan.ResultDigest {
		return errors.New("serve transport plan document drifted")
	}
	temporary, err := os.CreateTemp(filepath.Dir(plan.ConfigPath), config.InitializationTemporaryPrefix+"transport-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0600); err == nil {
		_, err = temporary.Write(plan.document)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, plan.ConfigPath); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(plan.ConfigPath))
	if err != nil {
		return err
	}
	err = directory.Sync()
	err = errors.Join(err, directory.Close())
	if err != nil {
		return err
	}
	inspection := config.Inspect(plan.ConfigPath)
	if inspection.State != config.StateValid || inspection.Config.API.TokenFile != plan.TokenPath || inspection.Config.API.UnixSocket != plan.UnixSocket || inspection.Config.API.Token == "" {
		return errors.New("published serve transport failed strict readback")
	}
	committed = true
	return nil
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func ensureOwnedDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 || !ok || int(stat.Uid) != os.Geteuid() {
		return errors.New("transport directory must be owner-only and owned by the current effective UID")
	}
	return nil
}

func writeExclusive(path string, value []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = file.Write(value); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
