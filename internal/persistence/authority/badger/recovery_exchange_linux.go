package badger

import "golang.org/x/sys/unix"

func recoveryExchangeSupported() error { return nil }
func exchangeRecoveryDirectories(a, b string) error {
	return unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_EXCHANGE)
}
