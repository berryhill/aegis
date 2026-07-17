package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/berryhill/aegis/internal/buildinfo"
	"github.com/berryhill/aegis/internal/command"
)

var version = buildinfo.Version

func main() { os.Exit(run()) }
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cmd := command.NewRoot(command.Dependencies{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Logger: log, Version: version})
	err := cmd.ExecuteContext(ctx)
	if err != nil {
		command.RenderError(os.Stderr, err)
	}
	return command.ExitCode(err)
}
