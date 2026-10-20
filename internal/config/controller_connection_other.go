//go:build !linux

package config

import "errors"

var ErrConnectionConfig = errors.New("controller_connection_unavailable")

func ConnectionSnapshot(string) ([]byte, error)                         { return nil, ErrConnectionConfig }
func ConnectControllerCodex(string, []byte, ControllerCodexLogin) error { return ErrConnectionConfig }
func ReadControllerCodexBinding(string) (*ControllerCodexLogin, error) {
	return nil, ErrConnectionConfig
}
