package command

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"reflect"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/initialize"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/principalauth"
	"github.com/berryhill/aegis/internal/reset"
)

type recoveryTransport struct {
	path     string
	cfg      config.Config
	digest   [32]byte
	verifier principalauth.Record
	review   *reset.OfflineTransportReview
}

func prepareRecoveryTransport(ctx context.Context, path string, cfg config.Config) (*recoveryTransport, error) {
	current, err := user.Current()
	if err != nil || current.Uid != cfg.Principal.UID || current.Username != cfg.Principal.User || cfg.Principal.ID == "" {
		return nil, principalauth.ErrAuthentication
	}
	verifier, err := principalauth.Load(filepath.Join(cfg.StateDir, "auth", principalauth.FileName))
	if err != nil {
		return nil, err
	}
	if verifier.PrincipalID != cfg.Principal.ID {
		return nil, principalauth.ErrAuthentication
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	review, err := reset.ReviewOfflineTransport(ctx, cfg.API.UnixSocket)
	if err != nil {
		return nil, err
	}
	return &recoveryTransport{path: path, cfg: cfg, digest: sha256.Sum256(data), verifier: verifier, review: review}, nil
}

func (r *recoveryTransport) admit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := user.Current()
	if err != nil || current.Uid != r.cfg.Principal.UID || current.Username != r.cfg.Principal.User {
		return principalauth.ErrAuthentication
	}
	cfg, err := config.Load(r.path, nil)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}
	if sha256.Sum256(data) != r.digest || !reflect.DeepEqual(cfg, r.cfg) {
		return errors.New("authority recovery configuration or principal changed after review")
	}
	verifier, err := principalauth.Load(filepath.Join(r.cfg.StateDir, "auth", principalauth.FileName))
	if err != nil {
		return err
	}
	if verifier != r.verifier {
		return principalauth.ErrVerifierChanged
	}
	return nil
}

func (r *recoveryTransport) recover(ctx context.Context, password []byte) (authoritybadger.InterruptedRecovery, error) {
	var result authoritybadger.InterruptedRecovery
	if err := r.admit(ctx); err != nil {
		return result, err
	}
	return initialize.RecoverInterruptedInstanceWithTransport(ctx, r.cfg, password, func(bounded context.Context, recoverStores func() error) error {
		return r.review.Run(bounded, func() error { return r.admit(bounded) }, recoverStores)
	})
}
