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
}

func newGatewayManagerClient(cfg config.Config) (*gatewayManagerClient, error) {
	if cfg.API.UnixSocket == "" || cfg.API.Token == "" {
		return nil, errors.New("control_plane_unavailable: protected Unix transport is not configured")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
	}}
	return &gatewayManagerClient{http: &http.Client{Transport: transport, Timeout: 30 * time.Second}, transport: cfg.API.Token}, nil
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
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&opened); err != nil || opened.ID == "" || opened.Token == "" || opened.ExpiresAt.IsZero() {
		return time.Time{}, errors.New("control plane returned an invalid manager session")
	}
	c.sessionID, c.token = opened.ID, opened.Token
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
	fmt.Fprintf(output, "AEGIS / manager — authenticated gateway client\nGateway owns authority and writable application state. Session expires %s.\nCommands: /status, /context, /help, /cancel, /exit\n", expires.UTC().Format(time.RFC3339))
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
		if !strings.HasPrefix(input, "/") {
			fmt.Fprintln(output, "Gateway manager accepts registry commands only; use /help. No authority action was attempted.")
			continue
		}
		result, executeErr := client.execute(cmd.Context(), input)
		if executeErr != nil {
			return executeErr
		}
		encoded, encodeErr := json.MarshalIndent(result, "", "  ")
		if encodeErr != nil {
			return encodeErr
		}
		fmt.Fprintln(output, string(encoded))
	}
}
