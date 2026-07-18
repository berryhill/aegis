package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/spf13/cobra"
)

type managerStartupResult struct {
	runtime *conversationalRuntime
	err     error
}

type managerStartupLine struct {
	line string
	eof  bool
	err  error
}

// startManagerWithQueue keeps one cancellation-safe terminal reader active
// while expensive runtime checks execute. Ordinary submitted messages are
// bounded and queued; local startup commands remain application-owned and are
// never sent to Hermes or the model.
func startManagerWithQueue(ctx context.Context, service *app.Service, subject core.Subject, guard *managerdomain.Guard, cmd *cobra.Command, input *terminalInput) (*conversationalRuntime, error, []string, context.CancelCauseFunc) {
	startupCtx, cancelStartup := context.WithCancelCause(ctx)
	stages := make(chan string, 16)
	result := make(chan managerStartupResult, 1)
	go func() {
		runtime, err := startConversationalManager(startupCtx, service, subject, guard, cmd, input, func(stage string) {
			select {
			case stages <- stage:
			case <-startupCtx.Done():
			}
		})
		result <- managerStartupResult{runtime: runtime, err: err}
	}()

	maximum := int(service.Config.Manager.Ingress.MaximumMessageBytes)
	var queued []string
	queuedBytes := 0
	readCtx, cancelRead := context.WithCancel(startupCtx)
	defer cancelRead()
	lines := make(chan managerStartupLine, 1)
	startRead := func() {
		go func(local context.Context) {
			line, eof, err := input.ReadLine(local, maximum)
			lines <- managerStartupLine{line: line, eof: eof, err: err}
		}(readCtx)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "[startup composer] Type a message to queue, or /help, /status, /clear, /quit.")
	fmt.Fprint(cmd.OutOrStdout(), "[startup composer] > ")
	startRead()
	for {
		select {
		case stage := <-stages:
			fmt.Fprintln(cmd.OutOrStdout(), "\n[startup]", stage)
		case completed := <-result:
			cancelRead()
			pending := <-lines
			if pending.err == nil && !pending.eof && strings.TrimSpace(pending.line) != "" {
				queued = appendStartupLine(cmd, queued, &queuedBytes, pending.line, maximum)
			}
			return completed.runtime, completed.err, queued, cancelStartup
		case read := <-lines:
			if read.err != nil || read.eof {
				cancelRead()
				cancelStartup(context.Canceled)
				completed := <-result
				return completed.runtime, context.Canceled, append(queued, "/quit"), cancelStartup
			}
			trimmed := strings.TrimSpace(read.line)
			switch trimmed {
			case "", "/status":
				readiness := inspectManagerReadiness(service)
				fmt.Fprintf(cmd.OutOrStdout(), "[startup status] runtime checks in progress; queued messages: %d; no queued message has been sent\nCredential authority: %s\nModel: %s\nArtifact: %s\nCertification: %s\nHermes: %s\nInference: %s\n", len(queued), readiness.authority, readiness.model, readiness.artifact, readiness.certification, readiness.hermes, readiness.inference)
			case "/help":
				fmt.Fprintln(cmd.OutOrStdout(), "Startup commands: /help /status /clear /quit /exit (plain quit and exit also work). Ordinary submitted messages are queued locally and remain unsent until startup completes. /audit and /secret metadata commands become available after startup.")
			case "/clear":
				fmt.Fprintln(cmd.OutOrStdout(), "Local startup display clear requested; runtime checks and queued messages are unchanged.")
			case "/quit", "/exit", "quit", "exit":
				cancelRead()
				cancelStartup(context.Canceled)
				completed := <-result
				return completed.runtime, context.Canceled, append(queued, "/quit"), cancelStartup
			default:
				if strings.HasPrefix(trimmed, "/") {
					fmt.Fprintln(cmd.OutOrStdout(), "That local command becomes available after startup; it was not sent or queued.")
				} else {
					queued = appendStartupLine(cmd, queued, &queuedBytes, read.line, maximum)
				}
			}
			readCtx, cancelRead = context.WithCancel(startupCtx)
			fmt.Fprint(cmd.OutOrStdout(), "[startup composer] > ")
			startRead()
		case <-ctx.Done():
			cancelRead()
			cancelStartup(context.Cause(ctx))
			completed := <-result
			return completed.runtime, context.Cause(ctx), queued, cancelStartup
		}
	}
}

func appendStartupLine(cmd *cobra.Command, queued []string, queuedBytes *int, line string, maximum int) []string {
	if len(queued) >= 16 || *queuedBytes+len(line) > min(maximum*4, 1<<20) {
		fmt.Fprintln(cmd.OutOrStdout(), "[startup queue] full; message was not retained or sent")
		return queued
	}
	queued = append(queued, line)
	*queuedBytes += len(line)
	fmt.Fprintf(cmd.OutOrStdout(), "[startup queue] queued locally as message %d; not sent\n", len(queued))
	return queued
}
