package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/core"
	managerdomain "github.com/berryhill/aegis/internal/manager"
	"github.com/berryhill/aegis/internal/slash"
	"github.com/berryhill/aegis/internal/tui"
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
func startManagerWithQueue(ctx context.Context, service *app.Service, subject core.Subject, guard *managerdomain.Guard, cmd *cobra.Command, input *terminalInput, presentation *tui.Controller, registry *slash.Registry) (*conversationalRuntime, error, []string, context.CancelCauseFunc) {
	startupCtx, cancelStartup := context.WithCancelCause(ctx)
	stages := make(chan string, 16)
	result := make(chan managerStartupResult, 1)
	go func() {
		runtime, err := startConversationalManager(startupCtx, service, subject, guard, cmd, input, presentation, func(stage string) {
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
	var lines chan managerStartupLine
	interactiveInput := presentation != nil && presentation.State().Capabilities.Profile != tui.Machine
	if interactiveInput {
		lines = make(chan managerStartupLine, 1)
	}
	startRead := func() {
		go func(local context.Context) {
			line, eof, err := input.ReadLine(local, maximum)
			lines <- managerStartupLine{line: line, eof: eof, err: err}
		}(readCtx)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "[AEGIS] Starting authenticated exact-local session. You can type now; submissions queue until ready.")
	fmt.Fprint(cmd.OutOrStdout(), "> ")
	if interactiveInput {
		startRead()
	}
	for {
		select {
		case <-stages:
			// Successful implementation-level checks stay quiet. Failures retain
			// their typed startup reason, and /status exposes readiness details.
		case completed := <-result:
			cancelRead()
			if interactiveInput {
				pending := <-lines
				if pending.err == nil && !pending.eof && strings.TrimSpace(pending.line) != "" {
					queued = appendStartupLine(cmd, queued, &queuedBytes, pending.line, maximum)
				}
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
			detection := slash.Detect(read.line)
			if trimmed == "" {
				readiness := inspectManagerReadiness(service)
				fmt.Fprintf(cmd.OutOrStdout(), "[startup status] runtime checks in progress; queued messages: %d; no queued message has been sent\nCredential authority: %s\nModel: %s\nArtifact: %s\nCertification: %s\nHermes: %s\nInference: %s\n", len(queued), readiness.authority, readiness.model, readiness.artifact, readiness.certification, readiness.hermes, readiness.inference)
			} else if trimmed == "quit" || trimmed == "exit" {
				cancelRead()
				cancelStartup(context.Canceled)
				completed := <-result
				return completed.runtime, context.Canceled, append(queued, "/quit"), cancelStartup
			} else if detection == slash.LiteralSlash {
				queued = appendStartupLine(cmd, queued, &queuedBytes, slash.UnescapeLiteral(read.line), maximum)
			} else if detection == slash.Command {
				request, parseErr := registry.Parse(read.line)
				if parseErr != nil {
					fmt.Fprintln(cmd.OutOrStdout(), "Local command rejected:", parseErr)
				} else {
					switch request.Canonical {
					case "status":
						readiness := inspectManagerReadiness(service)
						fmt.Fprintf(cmd.OutOrStdout(), "[startup status] runtime checks in progress; queued messages: %d; no queued message has been sent\nCredential authority: %s\nModel: %s\nArtifact: %s\nCertification: %s\nHermes: %s\nInference: %s\n", len(queued), readiness.authority, readiness.model, readiness.artifact, readiness.certification, readiness.hermes, readiness.inference)
					case "help":
						fmt.Fprintln(cmd.OutOrStdout(), startupRegistryHelp(registry))
					case "clear":
						fmt.Fprintln(cmd.OutOrStdout(), "Local startup display clear requested; runtime checks and queued messages are unchanged.")
					case "exit":
						cancelRead()
						cancelStartup(context.Canceled)
						completed := <-result
						return completed.runtime, context.Canceled, append(queued, "/exit"), cancelStartup
					default:
						available, reason := registry.Available(request.Definition, slash.Startup, map[string]bool{})
						fmt.Fprintf(cmd.OutOrStdout(), "Local /%s availability: %t (%s); it was not sent or queued.\n", request.Canonical, available, reason)
					}
				}
			} else {
				queued = appendStartupLine(cmd, queued, &queuedBytes, read.line, maximum)
			}
			readCtx, cancelRead = context.WithCancel(startupCtx)
			fmt.Fprint(cmd.OutOrStdout(), "> ")
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
	fmt.Fprintf(cmd.OutOrStdout(), "[startup] input %d accepted; it will run automatically when ready\n", len(queued))
	return queued
}

func startupRegistryHelp(registry *slash.Registry) string {
	var available []string
	for _, definition := range registry.Definitions() {
		if !definition.Base {
			continue
		}
		if ok, _ := registry.Available(definition, slash.Startup, map[string]bool{}); ok {
			available = append(available, "/"+definition.Name)
		}
	}
	return "Startup commands from the typed registry: " + strings.Join(available, " ") + ". plain quit and exit also work. Ordinary submitted messages are queued locally and remain unsent until startup completes."
}
