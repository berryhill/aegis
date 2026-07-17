package hermes

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/core"
)

const designSystemInstruction = `You are in an Aegis design-only session. You cannot authorize, provision, activate, write retained project artifacts, manage profiles, install plugins or MCP servers, or change external systems. Produce one complete Aegis charter JSON object matching the supplied requirements. Return only the JSON wrapped exactly in <aegis-charter> and </aegis-charter>. Do not use markdown fences.`

type gatewayMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Method  string `json:"method,omitempty"`
	Params  struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	} `json:"params,omitempty"`
	Result map[string]any `json:"result,omitempty"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// DesignProposal drives Hermes's documented TUI-gateway JSON-RPC stdio
// protocol directly. Hermes can only return proposal bytes; Aegis owns strict
// decoding, validation, canonicalization, persistence, and provisioning.
func (a *Adapter) DesignProposal(ctx context.Context, stateRoot, requirements string, retain bool, credentials []Credential) (proposal, home string, err error) {
	descriptor, err := a.Discover(ctx)
	if err != nil {
		return "", "", err
	}
	runtimeRoot := filepath.Join(stateRoot, "runtime")
	if err = os.MkdirAll(runtimeRoot, 0700); err != nil {
		return "", "", err
	}
	home, err = os.MkdirTemp(runtimeRoot, "design-")
	if err != nil {
		return "", "", err
	}
	if !retain {
		defer func() {
			if removeErr := os.RemoveAll(home); err == nil && removeErr != nil {
				err = removeErr
			}
		}()
	}
	python := gatewayPython(descriptor)
	if python == "" {
		return "", home, errors.New("Hermes TUI gateway Python executable not found")
	}
	command := exec.CommandContext(ctx, python, "-m", "tui_gateway.entry")
	command.Dir = home
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Env = append(minimalEnv(home, credentials),
		"HERMES_PYTHON_SRC_ROOT="+descriptor.Installation,
		"HERMES_TUI_TOOLSETS=web",
		"HERMES_TUI_SKILLS=",
		"HERMES_DISABLE_AUTO_SKILLS=1",
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		return "", home, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", home, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return "", home, err
	}
	if err = command.Start(); err != nil {
		return "", home, err
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	defer func() {
		_ = stdin.Close()
		if command.Process != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		}
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			if command.Process != nil {
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			}
			<-done
		}
	}()

	messages := make(chan gatewayMessage, 32)
	readErrors := make(chan error, 1)
	go readGateway(stdout, messages, readErrors)
	if _, err = waitGateway(ctx, messages, readErrors, func(message gatewayMessage) bool {
		return message.Method == "event" && message.Params.Type == "gateway.ready"
	}); err != nil {
		return "", home, fmt.Errorf("Hermes gateway startup: %w", err)
	}
	if err = writeGateway(stdin, "create", "session.create", map[string]any{"cols": 100, "source": "aegis-design"}); err != nil {
		return "", home, err
	}
	created, err := waitGateway(ctx, messages, readErrors, func(message gatewayMessage) bool { return fmt.Sprint(message.ID) == "create" })
	if err != nil {
		return "", home, err
	}
	if created.Error != nil {
		return "", home, fmt.Errorf("Hermes session.create failed: %s", created.Error.Message)
	}
	sessionID := fmt.Sprint(created.Result["session_id"])
	if sessionID == "" || sessionID == "<nil>" {
		sessionID = fmt.Sprint(created.Result["id"])
	}
	if sessionID == "" || sessionID == "<nil>" {
		return "", home, errors.New("Hermes session.create returned no session ID")
	}
	prompt := designSystemInstruction + "\n\nAuthenticated principal requirements:\n" + requirements
	if err = writeGateway(stdin, "prompt", "prompt.submit", map[string]any{"session_id": sessionID, "text": prompt}); err != nil {
		return "", home, err
	}
	var response strings.Builder
	started := false
	for {
		message, waitErr := waitGateway(ctx, messages, readErrors, func(message gatewayMessage) bool {
			return (message.Method == "event" && (message.Params.Type == "message.start" || message.Params.Type == "message.delta" || message.Params.Type == "message.complete" || message.Params.Type == "error")) || fmt.Sprint(message.ID) == "prompt"
		})
		if waitErr != nil {
			return "", home, waitErr
		}
		if message.Error != nil {
			return "", home, fmt.Errorf("Hermes prompt failed: %s", message.Error.Message)
		}
		switch message.Params.Type {
		case "message.start":
			started = true
		case "message.delta":
			if started {
				response.WriteString(payloadText(message.Params.Payload))
			}
		case "error":
			return "", home, fmt.Errorf("Hermes design turn failed: %s", payloadText(message.Params.Payload))
		case "message.complete":
			if !started {
				continue
			}
			if response.Len() == 0 {
				response.WriteString(payloadText(message.Params.Payload))
			}
			proposal, err = extractCharter(response.String())
			if err != nil {
				return "", home, err
			}
			return proposal, home, nil
		}
	}
}

func gatewayPython(descriptor core.RuntimeDescriptor) string {
	candidates := []string{
		filepath.Join(descriptor.Installation, "venv", "bin", "python"),
		filepath.Join(descriptor.Installation, ".venv", "bin", "python"),
		filepath.Join(filepath.Dir(descriptor.Executable), "python"),
		filepath.Join(filepath.Dir(descriptor.Executable), "python3"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

func readGateway(reader io.Reader, messages chan<- gatewayMessage, failures chan<- error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	for scanner.Scan() {
		var message gatewayMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		messages <- message
	}
	if err := scanner.Err(); err != nil {
		failures <- err
		return
	}
	failures <- io.EOF
}

func waitGateway(ctx context.Context, messages <-chan gatewayMessage, failures <-chan error, match func(gatewayMessage) bool) (gatewayMessage, error) {
	for {
		select {
		case <-ctx.Done():
			return gatewayMessage{}, ctx.Err()
		case err := <-failures:
			return gatewayMessage{}, err
		case message := <-messages:
			if match(message) {
				return message, nil
			}
		}
	}
}

func writeGateway(writer io.Writer, id, method string, params map[string]any) error {
	return json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}

func payloadText(payload map[string]any) string {
	for _, key := range []string{"delta", "text", "content", "message"} {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return "Hermes returned an unspecified error"
}

func extractCharter(output string) (string, error) {
	const open, close = "<aegis-charter>", "</aegis-charter>"
	start := strings.Index(output, open)
	end := strings.LastIndex(output, close)
	if start < 0 || end < 0 || end <= start+len(open) {
		return "", errors.New("Hermes response did not contain a structured Aegis charter proposal")
	}
	proposal := strings.TrimSpace(output[start+len(open) : end])
	if proposal == "" {
		return "", errors.New("Hermes returned an empty charter proposal")
	}
	return proposal, nil
}
