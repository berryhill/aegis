package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/onboarding"
	"github.com/berryhill/aegis/internal/userservice"
	"github.com/spf13/cobra"
)

func userServiceCmd(runner userservice.Runner, isTerminal func(io.Reader, io.Writer) bool, options *rootOptions) *cobra.Command {
	command := &cobra.Command{Use: "gateway", Aliases: []string{"service"}, Short: "Manage the authenticated Aegis gateway", Args: cobra.NoArgs}
	preview := func() (userservice.Plan, error) {
		executable, err := os.Executable()
		if err != nil {
			return userservice.Plan{}, err
		}
		return userservice.Preview(executable, options.configFile)
	}
	command.AddCommand(&cobra.Command{Use: "preview", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		plan, err := preview()
		if err != nil {
			return err
		}
		return output(cmd, plan)
	}})
	command.AddCommand(&cobra.Command{Use: "install", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New("gateway installation requires an interactive principal approval; no mutations were performed"))
		}
		plan, err := preview()
		if err != nil {
			return err
		}
		approved, err := approveServicePlan(cmd, plan, newTerminalInput(cmd.InOrStdin()))
		if err != nil || !approved {
			return err
		}
		if err = userservice.Apply(cmd.Context(), plan, runner, 20*time.Second); err != nil {
			return err
		}
		return output(cmd, userservice.LifecycleResult{Action: "install", Unit: userservice.UnitName, UnitPath: plan.UnitPath, UnitDigest: plan.UnitDigest, Active: true, Ready: true, AuditCurrent: true})
	}})
	command.AddCommand(&cobra.Command{Use: "status", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		plan, err := preview()
		if err != nil {
			return err
		}
		status, err := userservice.Status(cmd.Context(), runner, plan)
		if err != nil {
			return err
		}
		return output(cmd, status)
	}})
	for _, action := range []string{"start", "stop", "restart"} {
		action := action
		command.AddCommand(&cobra.Command{Use: action, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
				return usage(errors.New("gateway state changes require an interactive terminal"))
			}
			plan, err := preview()
			if err != nil {
				return err
			}
			result, err := userservice.Action(cmd.Context(), runner, plan, action, 20*time.Second)
			if err != nil {
				return err
			}
			return output(cmd, result)
		}})
	}
	command.AddCommand(&cobra.Command{Use: "uninstall", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !isTerminal(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return usage(errors.New("gateway uninstall requires an interactive principal approval; no mutations were performed"))
		}
		plan, err := preview()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Disable and remove exact Aegis-owned unit %s while preserving configuration, state, and external credentials? [y/N]: ", plan.UnitPath)
		answer, _, err := newTerminalInput(cmd.InOrStdin()).ReadLine(cmd.Context(), 16)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "yes" && strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Fprintln(cmd.OutOrStdout(), "Gateway uninstall declined; no mutations were performed.")
			return nil
		}
		if err = userservice.Uninstall(cmd.Context(), plan, runner); err != nil {
			return err
		}
		return output(cmd, userservice.LifecycleResult{Action: "uninstall", Unit: userservice.UnitName, UnitPath: plan.UnitPath, UnitDigest: plan.UnitDigest})
	}})
	return command
}

func reconcileServeTransport(cmd *cobra.Command, configPath string, input *terminalInput) (bool, error) {
	inspection := config.Inspect(configPath)
	if inspection.State != config.StateValid || inspection.Config.API.Token != "" && inspection.Config.API.UnixSocket != "" {
		return true, nil
	}
	plan, err := onboarding.PreviewTransport(configPath)
	if err != nil {
		return false, usage(err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "\nProtected control-plane transport reconciliation\nConfiguration: %s\nToken custody: %s (new owner-only file; value will not be displayed)\nUnix socket: %s\nOriginal digest: %s\nResult digest: %s\nApply this transport reconciliation? [Y/n]: ", plan.ConfigPath, plan.TokenPath, plan.UnixSocket, plan.OriginalDigest, plan.ResultDigest)
	approved, err := readDefaultYes(cmd, input)
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "Serve transport reconciliation declined; no mutations were performed.")
		return false, err
	}
	if err = onboarding.ApplyTransport(cmd.Context(), plan); err != nil {
		return false, err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Protected serve transport reconciled without displaying reusable bearer material.")
	return true, nil
}

func approveServicePlan(cmd *cobra.Command, plan userservice.Plan, input *terminalInput) (bool, error) {
	fmt.Fprintf(cmd.OutOrStdout(), "Aegis gateway installation preview\nPrincipal: %s\nUnit: %s\nDigest: %s\nExecutable: %s\nConfiguration: %s\nConsole: %s\nScope: systemctl --user; lingering will not be enabled.\nInstall and activate this gateway? [Y/n]: ", plan.Principal, plan.UnitPath, plan.UnitDigest, plan.Executable, plan.ConfigPath, plan.Origin)
	approved, err := readDefaultYes(cmd, input)
	if err != nil || !approved {
		fmt.Fprintln(cmd.OutOrStdout(), "Gateway installation declined; no gateway state was changed.")
		return false, err
	}
	return true, nil
}

func consoleCmd(options *rootOptions) *cobra.Command {
	return &cobra.Command{Use: "console", Short: "Obtain a single-use authenticated console bootstrap", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		result, err := obtainConsoleBootstrap(cmd.Context(), options.configFile)
		if err != nil {
			return err
		}
		return output(cmd, result)
	}}
}

func obtainConsoleBootstrap(ctx context.Context, configPath string) (map[string]any, error) {
	cfg, err := config.Load(configPath, nil)
	if err != nil {
		return nil, err
	}
	if cfg.API.UnixSocket == "" || cfg.API.Token == "" {
		return nil, errors.New("control_plane_unavailable: protected Unix transport is not configured")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
	}}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/console/bootstrap", bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+cfg.API.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("control_plane_unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("console bootstrap denied by control plane: HTTP %d", response.StatusCode)
	}
	var issued struct {
		Bootstrap string `json:"bootstrap"`
		ExpiresAt string `json:"expires_at"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&issued); err != nil || issued.Bootstrap == "" {
		return nil, errors.New("control plane returned an invalid console bootstrap")
	}
	return map[string]any{"console_origin": cfg.API.Console.Origin, "bootstrap": issued.Bootstrap, "expires_at": issued.ExpiresAt, "single_use": true, "reusable_bearer_exposed": false}, nil
}

type healthyGatewayAction string

const (
	healthyGatewayConsole  healthyGatewayAction = "console"
	healthyGatewayTerminal healthyGatewayAction = "terminal"
	healthyGatewayExit     healthyGatewayAction = "exit"
)

func chooseHealthyGatewayAction(cmd *cobra.Command, input *terminalInput) (healthyGatewayAction, error) {
	fmt.Fprint(cmd.OutOrStdout(), "Aegis gateway_healthy; the exact authenticated gateway owns operational authority.\nActions:\n  console  aegis console\n  terminal aegis manager\n  exit     exit\nChoose an action [console/terminal/exit] (default: exit): ")
	answer, eof, err := input.ReadLine(cmd.Context(), 32)
	if err != nil {
		return "", err
	}
	if eof || strings.TrimSpace(answer) == "" {
		return healthyGatewayExit, nil
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "console", "c":
		return healthyGatewayConsole, nil
	case "terminal", "t":
		return healthyGatewayTerminal, nil
	case "exit", "e":
		return healthyGatewayExit, nil
	default:
		return "", errors.New("healthy gateway action must be console, terminal, or exit; no mutation was performed")
	}
}

func launchConsole(cmd *cobra.Command, options *rootOptions, opener BrowserOpener) error {
	result, err := obtainConsoleBootstrap(cmd.Context(), options.configFile)
	if err != nil {
		return err
	}
	origin, _ := result["console_origin"].(string)
	target := strings.TrimRight(origin, "/") + "/console"
	bootstrap, _ := result["bootstrap"].(string)
	if err = launchBrowserSession(cmd.Context(), origin, bootstrap, opener); err != nil {
		result["browser_opened"] = false
		var handoffErr *browserSessionError
		if errors.As(err, &handoffErr) && handoffErr.bootstrapMayBeConsumed {
			delete(result, "bootstrap")
			result["manual_fallback_available"] = false
			result["bootstrap_may_be_consumed"] = true
			if outputErr := output(cmd, result); outputErr != nil {
				return errors.Join(err, outputErr)
			}
			return fmt.Errorf("browser session exchange failed after the single-use bootstrap may have been consumed; request a fresh bootstrap before retrying: %w", err)
		}
		result["manual_url"] = target
		if outputErr := output(cmd, result); outputErr != nil {
			return errors.Join(err, outputErr)
		}
		return fmt.Errorf("browser launch failed; open %s and enter the emitted single-use bootstrap manually: %w", target, err)
	}
	delete(result, "bootstrap")
	result["browser_opened"] = true
	result["browser_session_established"] = true
	return output(cmd, result)
}

type browserSessionError struct {
	err                    error
	bootstrapMayBeConsumed bool
}

func (e *browserSessionError) Error() string { return e.err.Error() }
func (e *browserSessionError) Unwrap() error { return e.err }

func launchBrowserSession(ctx context.Context, origin, bootstrap string, opener BrowserOpener) error {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname()) || bootstrap == "" {
		return errors.New("automatic browser handoff requires a plaintext loopback console and a valid bootstrap")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(parsed.Hostname(), "0"))
	if err != nil {
		return fmt.Errorf("start browser handoff: %w", err)
	}
	defer listener.Close()
	correlationBytes := make([]byte, 32)
	if _, err = rand.Read(correlationBytes); err != nil {
		return fmt.Errorf("generate browser handoff correlation: %w", err)
	}
	correlation := base64.RawURLEncoding.EncodeToString(correlationBytes)
	handoffPath := "/handoff/" + correlation
	confirmationPath := "/confirmed/" + correlation
	consoleURL := strings.TrimRight(origin, "/") + "/console"

	result := make(chan error, 1)
	var once sync.Once
	var claimed atomic.Bool
	var exchanged atomic.Bool
	var confirmed atomic.Bool
	var confirmationURL string
	complete := func(err error) { once.Do(func() { result <- err }) }
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Pragma", "no-cache")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		if request.Method == http.MethodGet && request.URL.Path == confirmationPath && request.URL.RawQuery == "" && exchanged.Load() && confirmed.CompareAndSwap(false, true) {
			writer.Header().Set("Location", consoleURL)
			writer.WriteHeader(http.StatusSeeOther)
			flusher, ok := writer.(http.Flusher)
			if !ok {
				complete(&browserSessionError{err: errors.New("flush browser confirmation redirect"), bootstrapMayBeConsumed: true})
				return
			}
			flusher.Flush()
			complete(nil)
			return
		}
		if request.Method != http.MethodGet || request.URL.Path != handoffPath || request.URL.RawQuery != "" || !claimed.CompareAndSwap(false, true) {
			http.NotFound(writer, request)
			return
		}
		body, marshalErr := json.Marshal(map[string]string{"bootstrap": bootstrap})
		if marshalErr != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			complete(errors.New("encode browser session exchange"))
			return
		}
		exchange, requestErr := http.NewRequestWithContext(request.Context(), http.MethodPost, strings.TrimRight(origin, "/")+"/console/session", bytes.NewReader(body))
		if requestErr != nil {
			writer.WriteHeader(http.StatusBadGateway)
			complete(errors.New("construct browser session exchange"))
			return
		}
		exchange.Header.Set("Content-Type", "application/json")
		exchange.Header.Set("Origin", origin)
		response, exchangeErr := client.Do(exchange)
		if exchangeErr != nil {
			writer.WriteHeader(http.StatusBadGateway)
			complete(&browserSessionError{err: fmt.Errorf("exchange browser session: %w", exchangeErr), bootstrapMayBeConsumed: true})
			return
		}
		defer response.Body.Close()
		cookies := response.Cookies()
		if response.StatusCode != http.StatusCreated || len(cookies) != 1 || cookies[0].Name != "aegis-console" || cookies[0].Value == "" || cookies[0].Path != "/console" || !cookies[0].HttpOnly || cookies[0].Domain != "" || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Secure != (parsed.Scheme == "https") {
			writer.WriteHeader(http.StatusBadGateway)
			complete(&browserSessionError{err: fmt.Errorf("console denied browser session exchange: HTTP %d", response.StatusCode), bootstrapMayBeConsumed: true})
			return
		}
		exchanged.Store(true)
		http.SetCookie(writer, cookies[0])
		target, targetErr := url.Parse(consoleURL)
		if targetErr != nil {
			writer.WriteHeader(http.StatusBadGateway)
			complete(&browserSessionError{err: errors.New("construct browser confirmation target"), bootstrapMayBeConsumed: true})
			return
		}
		query := target.Query()
		query.Set("browser_handoff", confirmationURL)
		target.RawQuery = query.Encode()
		writer.Header().Set("Location", target.String())
		writer.WriteHeader(http.StatusSeeOther)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			complete(fmt.Errorf("serve browser handoff: %w", serveErr))
		}
	}()
	_, handoffPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return fmt.Errorf("resolve browser handoff port: %w", err)
	}
	handoffURL := "http://" + net.JoinHostPort(parsed.Hostname(), handoffPort) + handoffPath
	confirmationURL = "http://" + net.JoinHostPort(parsed.Hostname(), handoffPort) + confirmationPath
	if err = opener(ctx, handoffURL); err != nil {
		return err
	}
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case err = <-result:
		return err
	case <-ctx.Done():
		if claimed.Load() {
			return &browserSessionError{err: ctx.Err(), bootstrapMayBeConsumed: true}
		}
		return ctx.Err()
	case <-timer.C:
		err = errors.New("browser did not complete the temporary handoff")
		if claimed.Load() {
			return &browserSessionError{err: err, bootstrapMayBeConsumed: true}
		}
		return err
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
