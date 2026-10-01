//go:build !linux

package userservice

import "context"

// No supported process-image identity probe exists on this platform.
func observeProcessImage(context.Context, Runner, string) string { return "unknown" }
