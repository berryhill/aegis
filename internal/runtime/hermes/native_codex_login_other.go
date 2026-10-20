//go:build !linux

package hermes

import (
	"context"
	"time"
)

func (a *Adapter) PreviewNativeCodexLogin(context.Context, string, time.Time, ...string) (NativeCodexMetadata, error) {
	return NativeCodexMetadata{}, ErrProviderAuthRuntimeUnqualified
}

func (a *Adapter) InspectNativeCodexCandidates(context.Context, string, time.Time) (NativeCodexCandidates, error) {
	return NativeCodexCandidates{}, ErrProviderAuthRuntimeUnqualified
}

// Native profile credential resolution is qualified only on Linux.
func (a *Adapter) ResolveNativeCodexLogin(context.Context, string, string, string, time.Time, ...string) (ProviderAccessToken, error) {
	return ProviderAccessToken{}, ErrProviderAuthRuntimeUnqualified
}
