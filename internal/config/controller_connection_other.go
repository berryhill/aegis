//go:build !linux

package config

import "errors"

var ErrConnectionConfig = errors.New("controller_connection_unavailable")

func ConnectionSnapshot(string) ([]byte, error) { return nil, ErrConnectionConfig }
func ConnectControllerCodex(string, []byte, ControllerCodexLogin, ...func() error) error {
	return ErrConnectionConfig
}
func ReplaceControllerCodex(string, []byte, ControllerCodexLogin, ControllerCodexLogin, ...func() error) error {
	return ErrConnectionConfig
}
func ReadControllerCodexBinding(string) (*ControllerCodexLogin, error) {
	return nil, ErrConnectionConfig
}

func ReadControllerCodexForReview(string) (*ControllerCodexLogin, ControllerCodexConfigReview, error) {
	return nil, ControllerCodexConfigReview{}, ErrConnectionConfig
}
func ReplaceControllerCodexReviewed(string, []byte, ControllerCodexLogin, ControllerCodexLogin, ControllerCodexConfigReview, ...func() error) error {
	return ErrConnectionConfig
}
