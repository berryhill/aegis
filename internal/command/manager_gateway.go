package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/managergateway"
	"github.com/berryhill/aegis/internal/slash"
	"github.com/berryhill/aegis/internal/tui"
	"github.com/spf13/cobra"
)

type gatewayManagerClient struct {
	http      *http.Client
	transport string
	sessionID string
	token     string
	mode      string
	reason    string
	nextStep  string
}

func newGatewayManagerClient(cfg config.Config) (*gatewayManagerClient, error) {
	if cfg.API.UnixSocket == "" || cfg.API.Token == "" {
		return nil, errors.New("control_plane_unavailable: protected Unix transport is not configured")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
	}}
	requestTimeout := cfg.Manager.Hermes.TurnTimeout
	startup := cfg.Manager.Inference.StartTimeout + 3*cfg.Manager.Inference.RequestTimeout + cfg.Manager.Hermes.GatewayStartTimeout
	if startup > requestTimeout {
		requestTimeout = startup
	}
	if requestTimeout < 30*time.Second {
		requestTimeout = 30 * time.Second
	}
	return &gatewayManagerClient{http: &http.Client{Transport: transport, Timeout: requestTimeout}, transport: cfg.API.Token}, nil
}

func (c *gatewayManagerClient) open(ctx context.Context) (time.Time, error) {
	request, err := c.request(ctx, http.MethodPost, "/v1/manager/sessions", map[string]any{})
	if err != nil {
		return time.Time{}, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return time.Time{}, fmt.Errorf("control_plane_unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return time.Time{}, managerGatewayStatusError("session authentication", response.StatusCode)
	}
	var opened struct {
		ID        string    `json:"id"`
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires"`
		Mode      string    `json:"mode"`
		Reason    string    `json:"reason"`
		NextStep  string    `json:"next_step"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&opened); err != nil || opened.ID == "" || opened.Token == "" || opened.ExpiresAt.IsZero() || (opened.Mode != "conversational" && opened.Mode != "degraded") {
		return time.Time{}, errors.New("control plane returned an invalid manager session")
	}
	c.sessionID, c.token, c.mode, c.reason, c.nextStep = opened.ID, opened.Token, opened.Mode, opened.Reason, opened.NextStep
	return opened.ExpiresAt, nil
}

func (c *gatewayManagerClient) execute(ctx context.Context, input string) (slash.Result, error) {
	request, err := c.request(ctx, http.MethodPost, "/v1/manager/sessions/"+c.sessionID+"/commands", map[string]string{"input": input})
	if err != nil {
		return slash.Result{}, err
	}
	request.Header.Set(managergateway.SessionHeader, c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return slash.Result{}, fmt.Errorf("manager gateway connection interrupted; rerun 'aegis manager' to establish a new authenticated session: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return slash.Result{}, managerGatewayStatusError("command authorization", response.StatusCode)
	}
	var result slash.Result
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil || result.Schema == "" || result.Operation == "" {
		return slash.Result{}, errors.New("control plane returned an invalid manager result")
	}
	return result, nil
}

func (c *gatewayManagerClient) turn(ctx context.Context, input string) (string, error) {
	request, err := c.request(ctx, http.MethodPost, "/v1/manager/sessions/"+c.sessionID+"/turns", map[string]string{"input": input})
	if err != nil {
		return "", err
	}
	request.Header.Set(managergateway.SessionHeader, c.token)
	response, err := c.http.Do(request)
	if err != nil {
		return "", fmt.Errorf("manager gateway connection interrupted; rerun 'aegis manager' to establish a new authenticated session: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", managerGatewayStatusError("conversational turn", response.StatusCode)
	}
	var result struct {
		Message string `json:"message"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil || strings.TrimSpace(result.Message) == "" {
		return "", errors.New("control plane returned an invalid manager turn")
	}
	return result.Message, nil
}

func (c *gatewayManagerClient) close(ctx context.Context) error {
	if c.sessionID == "" || c.token == "" {
		return nil
	}
	request, err := c.request(ctx, http.MethodDelete, "/v1/manager/sessions/"+c.sessionID, nil)
	if err != nil {
		return err
	}
	request.Header.Set(managergateway.SessionHeader, c.token)
	response, err := c.http.Do(request)
	c.sessionID, c.token = "", ""
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusUnauthorized {
		return managerGatewayStatusError("session close", response.StatusCode)
	}
	return nil
}

func (c *gatewayManagerClient) request(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			return nil, err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, &payload)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.transport)
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func managerGatewayStatusError(action string, status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict:
		return fmt.Errorf("manager gateway %s denied (HTTP %d); authentication or mandate is invalid, expired, or revoked; rerun 'aegis manager'", action, status)
	case http.StatusServiceUnavailable:
		return fmt.Errorf("manager gateway unavailable (HTTP %d); verify 'aegis gateway status'", status)
	default:
		return fmt.Errorf("manager gateway %s failed closed (HTTP %d)", action, status)
	}
}

func runGatewayManager(cmd *cobra.Command, configPath string) error {
	cfg, err := config.Load(configPath, nil)
	if err != nil {
		return err
	}
	client, err := newGatewayManagerClient(cfg)
	if err != nil {
		return err
	}
	expires, err := client.open(cmd.Context())
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = client.close(closeCtx)
	}()

	output := tui.NewSynchronizedWriter(cmd.OutOrStdout())
	capabilities := tui.Detect(cmd.InOrStdin(), cmd.OutOrStdout(), nil)
	composer := tui.NewComposer(cmd.InOrStdin(), output, int(cfg.Manager.Ingress.MaximumMessageBytes))
	if client.mode == "conversational" {
		fmt.Fprintf(output, "AEGIS / manager — authenticated conversational agent\nHermes Agent: active exact-local session. Gateway owns authority and writable application state.\nPrincipal: authenticated; stanza: secrets-manager; mandate expires %s.\nCommands: /status, /context, /help, /cancel, /exit\n", expires.UTC().Format(time.RFC3339))
	} else {
		fmt.Fprintf(output, "AEGIS / manager — degraded deterministic shell\nConversational Hermes Agent: unavailable (%s). No cloud or alternate-model fallback was attempted.\nNext: %s\nModel validation remains explicit; no artifact is downloaded, selected, configured, or certified by entering this shell.\nOptions: 'aegis manager model candidates'; 'aegis manager model discover --endpoint LOOPBACK_URL'; 'aegis manager model configure CANDIDATE_ID --endpoint LOOPBACK_URL'; 'aegis manager certify CANDIDATE_ID'.\nGateway owns authority and writable application state. Session expires %s.\nCommands: /status, /context, /help, /cancel, /exit\n", client.reason, client.nextStep, expires.UTC().Format(time.RFC3339))
	}
	for {
		input, eof, readErr := composer.Read(cmd.Context(), ">", capabilities)
		if readErr != nil {
			if errors.Is(readErr, tui.ErrInterrupted) || errors.Is(readErr, context.Canceled) {
				return nil
			}
			return readErr
		}
		input = strings.TrimSpace(input)
		if eof || input == "/exit" || input == "/quit" {
			return nil
		}
		if input == "" {
			continue
		}
		detection := slash.Detect(input)
		if detection != slash.Command {
			if client.mode != "conversational" {
				fmt.Fprintf(output, "Conversational local inference unavailable. Reason: %s\nNext: %s\nNo authority action or fallback was attempted.\n", client.reason, client.nextStep)
				continue
			}
			message, turnErr := client.turn(cmd.Context(), input)
			if turnErr != nil {
				return turnErr
			}
			renderGatewayConversation(output, message)
			continue
		}
		result, executeErr := client.execute(cmd.Context(), input)
		if executeErr != nil {
			return executeErr
		}
		renderGatewayResult(output, result)
	}
}

func renderGatewayConversation(output io.Writer, message string) {
	fmt.Fprintln(output, "Hermes model / untrusted")
	fmt.Fprintln(output, tui.Sanitize(message, tui.DefaultSanitizeOptions(tui.Prose)))
}

func renderGatewayResult(output io.Writer, result slash.Result) {
	if result.Operation == "manager.help" {
		fmt.Fprintln(output, "Available commands:")
		if commands, ok := result.Data["commands"].([]any); ok {
			for _, raw := range commands {
				command, ok := raw.(map[string]any)
				if !ok || command["available"] != true {
					continue
				}
				name := tui.Sanitize(fmt.Sprint(command["name"]), tui.DefaultSanitizeOptions(tui.SingleLine))
				usage := tui.Sanitize(fmt.Sprint(command["usage"]), tui.DefaultSanitizeOptions(tui.Prose))
				fmt.Fprintf(output, "  %-18s %s\n", name, usage)
			}
		}
		fmt.Fprintln(output, "Ordinary text talks to the Aegis agent only when the header reports an active Hermes Agent session.")
		return
	}
	operation := tui.Sanitize(result.Operation, tui.DefaultSanitizeOptions(tui.SingleLine))
	state := tui.Sanitize(result.State, tui.DefaultSanitizeOptions(tui.SingleLine))
	reason := tui.Sanitize(result.Reason, tui.DefaultSanitizeOptions(tui.SingleLine))
	fmt.Fprintf(output, "%s: %s (%s)\n", operation, state, reason)
	keys := make([]string, 0, len(result.Data))
	for key := range result.Data {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		safeKey := tui.Sanitize(key, tui.DefaultSanitizeOptions(tui.SingleLine))
		safeValue := tui.Sanitize(fmt.Sprint(result.Data[key]), tui.DefaultSanitizeOptions(tui.Prose))
		fmt.Fprintf(output, "  %s: %s\n", safeKey, safeValue)
	}
}
