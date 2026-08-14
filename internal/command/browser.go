package command

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

type BrowserOpener func(context.Context, string) error

func openBrowser(ctx context.Context, target string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "linux":
		name, args = "xdg-open", []string{target}
	case "darwin":
		name, args = "open", []string{target}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		return fmt.Errorf("browser launch is unsupported on platform %q", runtime.GOOS)
	}
	if err := exec.CommandContext(ctx, name, args...).Run(); err != nil {
		return fmt.Errorf("open browser with %s: %w", name, err)
	}
	return nil
}
