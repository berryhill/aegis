//go:build linux

package reset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// OfflineTransportReview pins a read-only transport observation. It confers no
// maintenance authority: Run requires fresh caller-owned authentication/admission.
type OfflineTransportReview struct {
	socket   string
	identity *Identity
}

// ReviewOfflineTransport rejects live or bound sockets before protected input.
// It never creates a lock, directory, quarantine, or store.
func ReviewOfflineTransport(ctx context.Context, socket string) (*OfflineTransportReview, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(socket) || filepath.Clean(socket) != socket {
		return nil, errors.New("exact offline transport required")
	}
	review := &OfflineTransportReview{socket: socket}
	if _, err := os.Lstat(socket); errors.Is(err, os.ErrNotExist) {
		return review, nil
	} else if err != nil {
		return nil, err
	}
	artifact, err := inspectOrphan(ctx, socket)
	if err != nil {
		return nil, err
	}
	review.identity = &artifact.identity
	return review, nil
}

func (r *OfflineTransportReview) revalidate(ctx context.Context, pinned string) error {
	if r == nil {
		return errors.New("offline transport review unavailable")
	}
	if r.identity == nil {
		if _, err := os.Lstat(pinned); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.New("transport appeared after offline review")
	}
	artifact, err := inspectOrphan(ctx, pinned)
	if err != nil {
		return err
	}
	if artifact.identity != *r.identity {
		return errors.New("transport changed after offline review")
	}
	return nil
}

// Run retains the existing API-startup lock across quarantine and validation.
// Configuration, principal, verifier and authentication freshness belong in admit.
func (r *OfflineTransportReview) Run(ctx context.Context, admit, validate func() error) error {
	if r == nil {
		return errors.New("offline transport review unavailable")
	}
	return withOfflineTransport(ctx, r.socket, admit, validate, r)
}

// EvidencePath reports custody only when the exact reviewed inode is preserved.
// It is not a recovery-success verdict.
func (r *OfflineTransportReview) EvidencePath() string {
	if r == nil || r.identity == nil {
		return ""
	}
	path := fmt.Sprintf("%s.upgrade-orphan-%x-%x", r.socket, r.identity.Device, r.identity.Inode)
	info, err := os.Lstat(path)
	if err != nil {
		return ""
	}
	id, err := socketIdentity(info)
	if err != nil || id != *r.identity {
		return ""
	}
	return path
}
