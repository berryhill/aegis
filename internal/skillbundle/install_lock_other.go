//go:build !linux && !darwin

package skillbundle

func lockInventory(string) (func(), error) {
	return nil, deny("installation_platform_unavailable", "", "managed atomic inventory currently supports Linux and macOS only")
}
