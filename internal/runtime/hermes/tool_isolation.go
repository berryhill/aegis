package hermes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

// no_mcp is retained as an Aegis authority spelling, not passed to Hermes:
// the TUI gateway treats unknown toolsets as a request for its CLI defaults.
// context_engine is a registered empty toolset in Hermes >=0.18.0. It is not
// trusted to remain empty: every zero-tool gateway must prove that before use.
const emptyToolset = "context_engine"

var errUnexpectedToolEvent = errors.New("Hermes tool isolation: unexpected tool event")

// waitTurnGateway detects tool events on an empty-tool turn before ordinary
// event filtering can discard them. Native tool callbacks carry params.session_id;
// a missing ID on this private stream cannot establish an unrelated session.
// Explicit foreign sessions retain the existing unrelated-event behavior.
// This is detection, not prevention of a runtime tool's effects. In particular,
// it must not apply to authorized tool-bearing (including broker) sessions.
func waitTurnGateway(ctx context.Context, messages <-chan gatewayMessage, failures <-chan error, sessionID string, emptyTools bool, match func(gatewayMessage) bool) (gatewayMessage, error) {
	denied := func(m gatewayMessage) bool {
		return emptyTools && m.Method == "event" && strings.HasPrefix(m.Params.Type, "tool.") && (m.Params.SessionID == sessionID || m.Params.SessionID == "")
	}
	message, err := waitGateway(ctx, messages, failures, func(m gatewayMessage) bool {
		return denied(m) || match(m)
	})
	if err == nil && denied(message) {
		return gatewayMessage{}, errUnexpectedToolEvent
	}
	return message, err
}

func launchToolsets(tools []string) string {
	var selected []string
	for _, tool := range tools {
		if tool != "no_mcp" {
			selected = append(selected, tool)
		}
	}
	if len(selected) == 0 {
		return emptyToolset
	}
	return strings.Join(selected, ",")
}

func verifyEmptyGateway(ctx context.Context, stdin io.Writer, messages <-chan gatewayMessage, failures <-chan error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := writeGateway(stdin, "aegis-tools", "tools.show", map[string]any{}); err != nil {
		return fmt.Errorf("Hermes tool isolation: %w", err)
	}
	response, err := waitGateway(ctx, messages, failures, func(m gatewayMessage) bool { return fmt.Sprint(m.ID) == "aegis-tools" })
	if err != nil {
		return fmt.Errorf("Hermes tool isolation: %w", err)
	}
	total, totalOK := response.Result["total"].(float64)
	sections, sectionsOK := response.Result["sections"].([]any)
	if response.Error != nil || !totalOK || total != 0 || !sectionsOK || len(sections) != 0 {
		return errors.New("Hermes tool isolation: expected zero tools and empty sections")
	}
	return nil
}

// Interactive CLI launches cannot inspect JSON-RPC on their terminal stream.
// Probe the exact installation and disposable home before attaching user input.
func probeEmptyGateway(ctx context.Context, descriptor core.RuntimeDescriptor, home string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	launch, err := discoverGatewayCommand(ctx, descriptor, home)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, launch.argv[0], launch.argv[1:]...)
	cmd.Dir = home
	cmd.Env = append(minimalEnv(home, nil), "HERMES_PYTHON_SRC_ROOT="+descriptor.Installation, "HERMES_TUI_TOOLSETS="+emptyToolset, "HERMES_SAFE_MODE=1", "HERMES_IGNORE_USER_CONFIG=1", "HERMES_IGNORE_RULES=1", "HERMES_TUI_SKILLS=", "HERMES_DISABLE_AUTO_SKILLS=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err = launch.fresh(); err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer stopAttemptProcess(cmd, stdin, done)
	messages := make(chan gatewayMessage, 128)
	failures := make(chan error, 1)
	go readGateway(stdout, messages, failures)
	if _, err = waitGateway(ctx, messages, failures, func(m gatewayMessage) bool { return m.Method == "event" && m.Params.Type == "gateway.ready" }); err != nil {
		return fmt.Errorf("Hermes tool isolation: %w", err)
	}
	return verifyEmptyGateway(ctx, stdin, messages, failures)
}
