//go:build !linux

package reset

import (
	"context"
	"errors"
)

type OfflineTransportReview struct{}

func ReviewOfflineTransport(context.Context, string) (*OfflineTransportReview, error) {
	return nil, errors.New("authenticated offline transport quarantine requires Linux")
}
func (*OfflineTransportReview) Run(context.Context, func() error, func() error) error {
	return errors.New("authenticated offline transport quarantine requires Linux")
}
func (*OfflineTransportReview) EvidencePath() string { return "" }
