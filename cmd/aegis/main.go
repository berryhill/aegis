package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/berryhill/aegis/internal/buildinfo"
	"github.com/berryhill/aegis/internal/command"
	managerdomain "github.com/berryhill/aegis/internal/manager"
)

var version = buildinfo.Version

func main() { os.Exit(run()) }
func run() int {
	ctx, stopSignals := managerSignalContext()
	defer stopSignals()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cmd := command.NewRoot(command.Dependencies{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Logger: log, Version: version})
	err := cmd.ExecuteContext(ctx)
	if err != nil {
		command.RenderError(os.Stderr, err)
	}
	return command.ExitCode(err)
}

func managerSignalContext() (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	finished := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(finished); signal.Stop(signals); cancel(nil) }) }
	go func() {
		select {
		case received := <-signals:
			// Restore normal OS behavior before requesting graceful cleanup. A
			// second SIGINT therefore remains a reliable hard-stop boundary.
			signal.Stop(signals)
			if received == syscall.SIGTERM {
				cancel(managerdomain.ErrTermination)
			} else {
				cancel(managerdomain.ErrInterrupt)
			}
		case <-finished:
		}
	}()
	return ctx, stop
}
