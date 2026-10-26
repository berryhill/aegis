//go:build !linux

package recoveryphysical

import "errors"

func recoveryExchangeSupported() error {
	return errors.New("interrupted recovery requires Linux atomic directory exchange; unsupported platform")
}
func Exchange(a, b string) error { return recoveryExchangeSupported() }
