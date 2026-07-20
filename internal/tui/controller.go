package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type Controller struct {
	mu                sync.Mutex
	out               io.Writer
	state             State
	now               func() time.Time
	queue             *Queue
	assistantRendered string
	assistantActive   bool
	progressActive    bool
	plainProgress     bool
}

func NewController(output io.Writer, capabilities Capabilities, security SecurityContext) *Controller {
	return &Controller{out: output, state: NewState(capabilities, security), now: time.Now, queue: NewQueue(256)}
}

func (controller *Controller) State() State {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	copyState := controller.state
	copyState.Components = append([]Component(nil), controller.state.Components...)
	return copyState
}

func (controller *Controller) SetCapabilities(capabilities Capabilities) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.state.Capabilities = capabilities
}

func (controller *Controller) Emit(event Event) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if !controller.queue.Push(event) {
		return fmt.Errorf("presentation event rejected: %s", event.Kind)
	}
	queued, ok := controller.queue.Pop(context.Background())
	if !ok {
		return fmt.Errorf("presentation event queue closed")
	}
	event = queued
	if event.At.IsZero() {
		event.At = controller.now().UTC()
	}
	controller.state = Update(controller.state, event)
	return controller.renderEvent(event)
}

func (controller *Controller) WriteAuthoritative(text string) error {
	return controller.Emit(Event{Kind: TerminalWarning, Origin: AegisAuthoritative, Message: text})
}

func (controller *Controller) RenderHeader() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	security := controller.state.Security
	width := controller.state.Capabilities.Width
	line := func(label, value string) string { return label + ":" + safeField(value, max(width-len(label)-1, 8)) }
	var rows []string
	if width < 50 {
		rows = []string{"AEGIS / authoritative", line("principal", security.Principal), line("stanza", security.Stanza), line("runtime", security.Runtime), line("authority", security.MandateState), line("route", security.Route)}
	} else {
		expiry := "unknown"
		if !security.ExpiresAt.IsZero() {
			expiry = security.ExpiresAt.Format(time.RFC3339)
		}
		rows = []string{fmt.Sprintf("AEGIS  principal:%s  stanza:%s", safeField(security.Principal, 24), safeField(security.Stanza, 24)), fmt.Sprintf("authority:%s  runtime:%s/%s  route:%s  expires:%s", safeField(security.MandateState, 16), safeField(security.Runtime, 20), safeField(security.RuntimeState, 12), safeField(security.Route, 18), expiry)}
	}
	if security.NoFallback {
		rows = append(rows, "policy: local-only / no fallback / no model switching")
	}
	_, err := fmt.Fprintln(controller.out, strings.Join(rows, "\n"))
	return err
}

func (controller *Controller) RenderStatus() error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	s := controller.state.Security
	values := []struct{ label, value string }{
		{"Origin", "AEGIS / authoritative"}, {"Principal", s.Principal}, {"Trust stanza / security context", s.Stanza},
		{"Mandate", s.MandateID}, {"Authority", s.MandateState}, {"Runtime", s.Runtime + " / " + s.RuntimeState},
		{"Inference", s.RuntimeState},
		{"Route", s.Route}, {"Model", s.Model}, {"Model digest", s.ModelDigest}, {"Certification", s.Certification},
		{"Policy digest", s.PolicyDigest}, {"Credential authority", s.Authority},
	}
	for _, value := range values {
		if _, err := fmt.Fprintf(controller.out, "%s: %s\n", value.label, safeField(value.value, 4096)); err != nil {
			return err
		}
	}
	if !s.ExpiresAt.IsZero() {
		_, _ = fmt.Fprintln(controller.out, "Expires:", s.ExpiresAt.Format(time.RFC3339))
	}
	_, err := fmt.Fprintln(controller.out, "No cloud fallback: enabled\nModel switching: disabled\nIsolation: disposable runtime state is not a host sandbox")
	return err
}

func (controller *Controller) Redraw() error {
	controller.mu.Lock()
	rich := controller.state.Capabilities.Profile == RichInteractive && controller.state.Capabilities.Term != "dumb"
	if rich {
		_, _ = io.WriteString(controller.out, "\x1b[2J\x1b[H")
	}
	controller.mu.Unlock()
	return controller.RenderHeader()
}

func (controller *Controller) renderEvent(event Event) error {
	message := event.Message
	if message == "" {
		message = event.Reason
	}
	if message == "" && event.Stage != "" {
		message = event.Stage
	}
	if message == "" {
		if len(event.Fields) == 0 {
			return nil
		}
	}
	context := Prose
	if event.Origin == AegisAuthoritative {
		context = SecurityField
	}
	message = Sanitize(message, DefaultSanitizeOptions(context))
	label := map[Origin]string{AegisAuthoritative: "AEGIS / authoritative", AegisDiagnostic: "AEGIS / diagnostic", RuntimeHermes: "Hermes Agent / runtime", ModelUntrusted: "Hermes model / untrusted", UserInput: "You / guarded input"}[event.Origin]
	if controller.state.Capabilities.Profile == PlainInteractive {
		label = "[origin: " + label + "]"
	} else {
		label = "[" + label + "]"
	}
	if event.Kind == TurnProgress {
		if controller.assistantActive {
			return nil
		}
		if controller.state.Capabilities.Profile == RichInteractive && controller.state.Capabilities.Term != "dumb" {
			controller.progressActive = true
			_, err := fmt.Fprintf(controller.out, "\r\x1b[2K%s %s", label, message)
			return err
		}
		if controller.plainProgress {
			return nil
		}
		controller.plainProgress = true
	}
	if event.Kind == AssistantDelta {
		if controller.progressActive {
			if _, err := io.WriteString(controller.out, "\r\x1b[2K"); err != nil {
				return err
			}
			controller.progressActive = false
		}
		if !controller.assistantActive {
			if _, err := fmt.Fprint(controller.out, label, " "); err != nil {
				return err
			}
			controller.assistantActive = true
			controller.assistantRendered = ""
		}
		if !strings.HasPrefix(message, controller.assistantRendered) {
			return nil
		}
		delta := strings.TrimPrefix(message, controller.assistantRendered)
		controller.assistantRendered = message
		_, err := io.WriteString(controller.out, delta)
		return err
	}
	if event.Kind == AssistantCompleted && controller.assistantActive {
		controller.assistantActive = false
		controller.assistantRendered = ""
		controller.plainProgress = false
		_, err := io.WriteString(controller.out, "\n")
		return err
	}
	if event.Kind == AssistantRejected && controller.assistantActive {
		controller.assistantActive = false
		controller.assistantRendered = ""
		controller.plainProgress = false
		if _, err := io.WriteString(controller.out, "\n"); err != nil {
			return err
		}
	}
	if controller.progressActive && event.Kind != TurnProgress {
		if _, err := io.WriteString(controller.out, "\r\x1b[2K"); err != nil {
			return err
		}
		controller.progressActive = false
	}
	if event.Kind == TurnCompleted || event.Kind == TurnFailed || event.Kind == TurnInterrupted {
		controller.plainProgress = false
	}
	if len(event.Fields) != 0 {
		encoded, err := json.MarshalIndent(event.Fields, "", "  ")
		if err != nil {
			return err
		}
		fields := Sanitize(string(encoded), DefaultSanitizeOptions(Prose))
		if message == "" {
			message = fields
		} else {
			message += "\n" + fields
		}
	}
	_, err := fmt.Fprintf(controller.out, "%s %s\n", label, message)
	return err
}

func safeField(value string, maximum int) string {
	value = Sanitize(value, SanitizeOptions{Context: SecurityField, MaxBytes: maximum * 4, MaxRunes: maximum, MaxLines: 1, MaxWidth: maximum})
	items := []rune(value)
	if len(items) > maximum {
		return string(items[:max(maximum-1, 1)]) + "…"
	}
	if value == "" {
		return "unknown"
	}
	return value
}

func ApprovalCard(operation, target, actor, stanza, consequence, phrase string, expires time.Time, width int, ascii bool) string {
	if width < 40 {
		width = 40
	}
	operation, target, actor, stanza, consequence, phrase = safeField(operation, 4096), safeField(target, 4096), safeField(actor, 4096), safeField(stanza, 4096), safeField(consequence, 4096), safeField(phrase, 4096)
	border, corner := "─", "╭"
	if ascii {
		border, corner = "-", "+"
	}
	header := corner + strings.Repeat(border, max(width-2, 1))
	return fmt.Sprintf("%s\nAEGIS / AUTHORITATIVE APPROVAL\nOperation: %s\nTarget (full, untruncated): %s\nActor: %s\nTrust stanza: %s\nConsequence: %s\nAuthority expires: %s\nSafe default: CANCEL\nAllowed: exact phrase or cancel\nType exactly: %s\n%s", header, operation, target, actor, stanza, consequence, expires.Format(time.RFC3339), phrase, header)
}
