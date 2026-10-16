//go:build !linux

package badger

import "errors"

func recoveryExchangeSupported() error {
	return errors.New("interrupted recovery requires Linux atomic directory exchange; unsupported platform")
}
func exchangeRecoveryDirectories(a, b string) error { return recoveryExchangeSupported() }
