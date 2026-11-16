package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
)

// All fixtures in this file are synthetic stdio gateways: no native tools,
// credentials, host listeners, provider requests or transport qualification.
const toolEventCanary = "SYNTHETIC_PRIVATE_TOOL_PAYLOAD"

func syntheticToolEvent(kind, session string) string {
	params := map[string]any{"type": kind, "payload": map[string]any{"name": toolEventCanary, "args": toolEventCanary, "result": toolEventCanary}}
	if session != "" {
		params["session_id"] = session
	}
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "event", "params": params})
	return "printf '%s\\n' '" + string(data) + "'\n"
}

func syntheticToolGateway(before, after, terminal string) string {
	return `#!/bin/sh
printf '%s\n' "$$" > "$HERMES_PYTHON_SRC_ROOT/gateway.pid"
printf '%s\n' '{"method":"event","params":{"type":"gateway.ready"}}'
if [ "$HERMES_TUI_TOOLSETS" = context_engine ]; then
  read tools
  printf '%s\n' '{"id":"aegis-tools","result":{"total":0,"sections":[]}}'
fi
read create
printf '%s\n' '{"id":"create","result":{"session_id":"selected"}}'
read prompt
` + before + `printf '%s\n' '{"id":"prompt","result":{"accepted":true}}'
printf '%s\n' '{"method":"event","params":{"type":"message.start","session_id":"selected","payload":{}}}'
printf '%s\n' '{"method":"event","params":{"type":"message.delta","session_id":"selected","payload":{"text":"SYNTHETIC_PRIVATE_PREVIEW"}}}'
` + after + terminal + "\nwhile read rest; do :; done\n"
}

const syntheticToolComplete = `printf '%s\n' '{"method":"event","params":{"type":"message.complete","session_id":"selected","payload":{"status":"complete","text":"SYNTHETIC_OK"}}}'`

func assertSyntheticGatewayClean(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "state", "runtime"))
	if err != nil || len(entries) != 0 {
		t.Fatal("disposable runtime home not removed")
	}
	data, err := os.ReadFile(filepath.Join(root, "install", "gateway.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatal("invalid synthetic child pid")
	}
	if err = syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("synthetic child not reaped")
	}
}

func TestAttemptToolEventsDenyWithoutOutput(t *testing.T) {
	// Native names are start/complete/generating/progress/output_risk. call/end
	// reproduce the independent audit's additional unknown tool.* cases.
	for _, kind := range []string{"tool.start", "tool.complete", "tool.generating", "tool.progress", "tool.output_risk", "tool.call", "tool.end", "tool.future"} {
		for _, session := range []string{"selected", ""} {
			for _, beforeStart := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/session=%s/before=%t", kind, session, beforeStart), func(t *testing.T) {
					before, after := "", syntheticToolEvent(kind, session)
					if beforeStart {
						before, after = after, ""
					}
					a, root := attemptTestAdapter(t, syntheticToolGateway(before, after, syntheticToolComplete))
					var logs bytes.Buffer
					a.log = slog.New(slog.NewTextHandler(&logs, nil))
					result, err := a.AttemptTurn(context.Background(), validAttemptTurnRequest(root))
					if !errors.Is(err, ErrAttemptDenied) || result.Attempt.State != execution.StateDenied || result.Attempt.Reason != "unexpected_tool_event" || result.Output != "" || result.Attempt.FinishedAt == nil {
						t.Fatal("tool event did not produce closed denial without output")
					}
					if !IsAttemptFailureReason(result.Attempt.Reason) {
						t.Fatal("closed controller failure vocabulary lost denial reason")
					}
					serialized, marshalErr := json.Marshal(result)
					if marshalErr != nil || strings.Contains(string(serialized)+logs.String()+err.Error(), toolEventCanary) || strings.Contains(string(serialized), "SYNTHETIC_PRIVATE_PREVIEW") {
						t.Fatal("private runtime bytes escaped denial")
					}
					assertSyntheticGatewayClean(t, root)
				})
			}
		}
	}
}

func TestAttemptToolEventsPreserveAuthorizedAndBenignTraffic(t *testing.T) {
	for _, toolsEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("tools=%t", toolsEnabled), func(t *testing.T) {
			events := ""
			for _, kind := range []string{"status.update", "thinking.delta", "reasoning.delta", "session.usage", "usage.update", "session.info", "notification.show"} {
				events += syntheticToolEvent(kind, "selected")
				events += syntheticToolEvent(kind, "")
			}
			for _, kind := range []string{"tool.start", "tool.complete", "tool.generating", "tool.progress", "tool.output_risk"} {
				events += syntheticToolEvent(kind, "foreign-session")
				if toolsEnabled {
					events += syntheticToolEvent(kind, "selected")
				}
			}
			a, root := attemptTestAdapter(t, syntheticToolGateway(events, events, syntheticToolComplete))
			request := validAttemptTurnRequest(root)
			if toolsEnabled {
				request.Launch.Mandate.Tools = []string{"terminal"}
				request.Launch.Mandate.Hermes.Toolsets = []string{"terminal"}
				request.Launch.AuthorityContext.Authority.Tools = []string{"terminal"}
				request.Launch.AuthorityContext.Authority.Hermes = request.Launch.Mandate.Hermes
				request.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(request.Launch.AuthorityContext)
			}
			result, err := a.AttemptTurn(context.Background(), request)
			if err != nil || result.Attempt.State != execution.StateSucceeded || result.Output != "SYNTHETIC_OK" {
				t.Fatalf("authorized/benign events changed completion: state=%s err=%v", result.Attempt.State, err)
			}
			assertSyntheticGatewayClean(t, root)
		})
	}
}

func TestAttemptToolEventReaderCleanup(t *testing.T) {
	for _, mode := range []string{"flood", "error", "cancel", "term-resistant"} {
		t.Run(mode, func(t *testing.T) {
			after, terminal := "", ""
			switch mode {
			case "flood":
				after = syntheticToolEvent("tool.start", "selected") + "while :; do\n" + syntheticToolEvent("tool.complete", "selected") + "done\n"
			case "error":
				after = syntheticToolEvent("error", "selected")
			case "term-resistant":
				after = "trap '' TERM\n" + syntheticToolEvent("tool.start", "selected") + "while :; do :; done\n"
			}
			a, root := attemptTestAdapter(t, syntheticToolGateway("", after, terminal))
			request := validAttemptTurnRequest(root)
			request.Bounds.Duration = 5 * time.Second
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if mode == "cancel" {
				request.SessionCapability = func(context.Context, execution.Turn) error {
					cancel()
					return nil
				}
			}
			start := time.Now()
			result, err := a.AttemptTurn(ctx, request)
			if err == nil || result.Attempt.State == execution.StateSucceeded || result.Output != "" {
				t.Fatal("failed or cancelled gateway returned success/output")
			}
			if mode == "flood" || mode == "term-resistant" {
				if !errors.Is(err, ErrAttemptDenied) || result.Attempt.Reason != "unexpected_tool_event" {
					t.Fatal("tool event denial lost")
				}
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("cleanup exceeded bounded termination interval")
			}
			assertSyntheticGatewayClean(t, root)
		})
	}
}

func TestDesignToolEventsDenyWithoutProposal(t *testing.T) {
	for _, beforeStart := range []bool{true, false} {
		before, after := "", syntheticToolEvent("tool.start", "selected")
		if beforeStart {
			before, after = after, ""
		}
		a, root := attemptTestAdapter(t, syntheticToolGateway(before, after, syntheticToolComplete))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		proposal, _, err := a.DesignProposal(ctx, filepath.Join(root, "state"), "synthetic design", false, nil)
		cancel()
		if !errors.Is(err, errUnexpectedToolEvent) || proposal != "" || strings.Contains(err.Error(), toolEventCanary) {
			t.Fatal("empty-tool design accepted tool event or leaked payload")
		}
		assertSyntheticGatewayClean(t, root)
	}
}

func TestGatewayReaderCancellationUnblocksDelivery(t *testing.T) {
	for _, input := range []string{"", `{"method":"event","params":{"type":"tool.start"}}` + "\n"} {
		ctx, cancel := context.WithCancel(context.Background())
		messages := make(chan gatewayMessage)
		failures := make(chan error)
		done := make(chan struct{})
		go func() {
			defer close(done)
			readGatewayContext(ctx, strings.NewReader(input), messages, failures)
		}()
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("reader retained after delivery cancellation")
		}
	}
}
