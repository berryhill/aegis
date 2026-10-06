package command

import (
	"context"
	"path/filepath"
)

// Direct protected helper input only: no Acquire/terminal fallback, stdin,
// caller-selected executable, password argument or environment secret.
func acquireDoerSetupPassword(ctx context.Context) ([]byte, error) {
	provider := newAuthorityPassphraseService(nil)
	if provider.getenv("DISPLAY") == "" && provider.getenv("WAYLAND_DISPLAY") == "" {
		return nil, &PassphraseError{Kind: PassphraseUnavailable, reason: "independent native display unavailable"}
	}
	helper := trustedConfirmationHelper()
	switch filepath.Base(helper) {
	case "pinentry-gnome3", "pinentry-qt", "pinentry-qt5", "pinentry-gtk-2", "pinentry-fltk":
	default:
		return nil, &PassphraseError{Kind: PassphraseUnavailable, reason: "independent protected desktop password helper unavailable"}
	}
	return provider.pinentry(ctx, helper, PrincipalPasswordAuthenticate, "principal-authenticate")
}
