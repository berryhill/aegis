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
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"github.com/berryhill/aegis/internal/principalauth"
)

// RecoverInterruptedAuthority is an offline application boundary. No full
// service, credential vault, fleet store, runtime or listener is opened.
// Authentication uses the configured OS principal AND enrolled principal
// password, never a caller-supplied subject or confirmation flag.
func RecoverInterruptedAuthority(ctx context.Context, cfg config.Config, password []byte) (authoritybadger.InterruptedRecovery, error) {
	return recoverInterrupted(ctx, cfg, password, false, nil)
}

// RecoverInterruptedInstance performs explicit offline physical recovery of the
// existing authority and fleet stores under ONE password proof and deadline.
// Domain resume and fresh role admission remain separate operations.
func RecoverInterruptedInstance(ctx context.Context, cfg config.Config, password []byte) (authoritybadger.InterruptedRecovery, error) {
	return recoverInterrupted(ctx, cfg, password, true, nil)
}

// RecoverInterruptedInstanceWithTransport authenticates once through the shared
// cancellable KDF guard, then runs the supplied transport boundary under the same
// fixed deadline as both stores. The boundary must call recover only while its
// startup exclusion is held. It cannot bypass the absent-transport store check.
func RecoverInterruptedInstanceWithTransport(ctx context.Context, cfg config.Config, password []byte, transport func(context.Context, func() error) error) (authoritybadger.InterruptedRecovery, error) {
	if transport == nil {
		clear(password)
		return authoritybadger.InterruptedRecovery{}, errors.New("recovery transport boundary required")
	}
	return recoverInterrupted(ctx, cfg, password, true, transport)
}

func recoverInterrupted(ctx context.Context, cfg config.Config, password []byte, fleet bool, transport func(context.Context, func() error) error) (authoritybadger.InterruptedRecovery, error) {
	var empty authoritybadger.InterruptedRecovery
	defer clear(password)
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := cfg.Validate(); err != nil {
		return empty, err
	}
	current, err := user.Current()
	if err != nil || current.Uid != cfg.Principal.UID || current.Username != cfg.Principal.User || cfg.Principal.ID == "" {
		return empty, principalauth.ErrAuthentication
	}
	if transport == nil && cfg.API.UnixSocket != "" {
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
	if err = verifyRecoveryPassword(ctx, password, verifier.Verify); err != nil {
		return empty, err
	}
	// Bound both lock acquisition and verification to fresh authentication.
	duration := cfg.Principal.AuthTTL
	if duration > 2*time.Minute {
		duration = 2 * time.Minute
	}
	bounded, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	var result authoritybadger.InterruptedRecovery
	run := func() error {
		if cfg.API.UnixSocket != "" {
			if _, err := os.Lstat(cfg.API.UnixSocket); !errors.Is(err, os.ErrNotExist) {
				return errors.New("authority recovery requires absent protected Unix transport")
			}
		}
		var err error
		result, err = authoritybadger.RecoverInterruptedAs(bounded, filepath.Join(cfg.StateDir, "persistence", "authority-v1"), cfg.Principal.ID)
		if err != nil || !fleet {
			return err
		}
		_, err = fleetbadger.RecoverInterruptedAs(bounded, filepath.Join(cfg.StateDir, "persistence", "fleet-v1"), cfg.Principal.ID)
		return err
	}
	if transport != nil {
		err = transport(bounded, run)
	} else {
		err = run()
	}
	if err != nil {
		result.Recovered = false
	}
	return result, err
}
