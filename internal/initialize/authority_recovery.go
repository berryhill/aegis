package initialize

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/berryhill/aegis/internal/config"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
)

// RecoverInterruptedAuthority is an offline application boundary. No full
// service, credential vault, fleet store, runtime or listener is opened.
// Authentication uses the configured OS principal AND enrolled principal
// password, never a caller-supplied subject or confirmation flag.
func RecoverInterruptedAuthority(ctx context.Context, cfg config.Config, password []byte) (authoritybadger.InterruptedRecovery, error) {
	var empty authoritybadger.InterruptedRecovery
	defer clear(password)
	if err := cfg.Validate(); err != nil {
		return empty, err
	}
	current, err := user.Current()
	if err != nil || current.Uid != cfg.Principal.UID || current.Username != cfg.Principal.User || cfg.Principal.ID == "" {
		return empty, principalauth.ErrAuthentication
	}
	if cfg.API.UnixSocket != "" {
		if _, err = os.Lstat(cfg.API.UnixSocket); !errors.Is(err, os.ErrNotExist) {
			return empty, errors.New("authority recovery requires absent protected Unix transport; stop the owning gateway first")
		}
	}
	path := filepath.Join(cfg.StateDir, "auth", principalauth.FileName)
	verifier, err := principalauth.Load(path)
	if err != nil {
		return empty, err
	}
	if verifier.PrincipalID != cfg.Principal.ID {
		return empty, principalauth.ErrAuthentication
	}
	if err = verifier.Verify(password); err != nil {
		return empty, err
	}
	// Bound both lock acquisition and verification to fresh authentication.
	duration := cfg.Principal.AuthTTL
	if duration > 2*time.Minute {
		duration = 2 * time.Minute
	}
	bounded, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	return authoritybadger.RecoverInterruptedAs(bounded, filepath.Join(cfg.StateDir, "persistence", "authority-v1"), cfg.Principal.ID)
}
