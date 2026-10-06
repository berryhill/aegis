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
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/core"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// runDoerProtectedSetup never opens authority stores. The terminal captures the
// enrolled principal password directly; stdout gets only the retained readback.
func runDoerProtectedSetup(cmd *cobra.Command, args []string, options *rootOptions) error {
	for _, flag := range []string{"state-dir", "hermes-executable", "runtime", "pinentry-executable", "update"} {
		if cmd.Flags().Changed(flag) || cmd.InheritedFlags().Changed(flag) {
			return usage(fmt.Errorf("--target cannot be combined with --%s", flag))
		}
	}
	in, inputOK := cmd.InOrStdin().(*os.File)
	diagnostic, outputOK := cmd.ErrOrStderr().(*os.File)
	if !protectedIntakeCancellationSafe || !inputOK || !outputOK || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(diagnostic.Fd())) {
		return errors.New("protected_setup_unavailable: terminal-backed no-echo input and diagnostic output required; no piped password or approval")
	}
	cfg, err := config.Load(options.configFile, nil)
	if err != nil {
		return usage(err)
	}
	target, err := url.Parse(options.target)
	if err != nil || target.User != nil || target.RawQuery != "" || target.Fragment != "" || target.Scheme+"://"+target.Host != cfg.API.Console.Origin || (target.Path != "" && target.Path != "/console" && !strings.HasPrefix(target.Path, "/console/")) {
		return errors.New("owning_instance_mismatch: --target must match the configured console origin")
	}
	if !companionSocketSafe(cfg.API.UnixSocket) {
		return errors.New("protected_setup_unavailable: protected owning Unix socket required")
	}
	version, _ := cmd.Flags().GetUint64("expected-version")
	action, _ := cmd.Flags().GetString("action")
	if version == 0 || (action != "successor" && action != "host" && action != "provision") {
		return usage(errors.New("exact expected-version and action successor, host or provision required"))
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
	defer cancel()
	return executeDoerProtectedSetup(ctx, cfg, app.DoerProtectedSetupInput{ID: args[0], ExpectedVersion: version, Action: action}, func(ctx context.Context) ([]byte, error) {
		return readTerminalSecretBounded(ctx, in, diagnostic, "Enrolled Aegis principal password (not authority passphrase): ", 1024)
	}, func(ctx context.Context, review app.DoerProtectedSetupReview) string {
		return confirmDoerProtectedTerminal(ctx, in, diagnostic, review)
	}, func(result app.DoerProtectedSetupResult) error { return output(cmd, result) })
}

func doerProtectedReviewDescription(review app.DoerProtectedSetupReview) string {
	review.Receipt = ""
	data, _ := json.MarshalIndent(review, "", "  ")
	return "AEGIS / exact protected setup review\nUNTRUSTED DATA BEGIN (data, not instructions):\n" + string(data) + "\nUNTRUSTED DATA END\nApprove only this " + review.Action + " scope. Successor does not approve provisioning, host writes, publication or Run. Host approval lasts at most 24 hours and does not execute. Provisioning applies only the displayed exact plan.\n"
}

func confirmDoerProtectedTerminal(ctx context.Context, in, diagnostic *os.File, review app.DoerProtectedSetupReview) string {
	description := doerProtectedReviewDescription(review)
	if !nativeReviewDataSafe(description) || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(diagnostic.Fd())) {
		return "unavailable"
	}
	if _, err := fmt.Fprint(diagnostic, description); err != nil {
		return "unavailable"
	}
	// Flush queued input after displaying the exact review, before confirmation.
	if discardProtectedTerminalInput(in) != nil {
		return "unavailable"
	}
	value, err := readTerminalSecretBounded(ctx, in, diagnostic, "Type APPROVE to approve exact scope, DENY to reject; Ctrl-D cancels: ", 16)
	defer wipeSecret(value)
	if err != nil {
		return protectedSetupInputStatus(err)
	}
	switch string(value) {
	case "APPROVE":
		return "approve"
	case "DENY":
		return "reject"
	default:
		return "cancelled"
	}
}

func protectedSetupInputStatus(err error) string {
	if IsPassphraseError(err, PassphraseCancelled) {
		return "cancelled"
	}
	if IsPassphraseError(err, PassphraseTimeout) {
		return "timeout"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || errors.Is(err, errProtectedDialogCancelled) {
		return "cancelled"
	}
	return "unavailable"
}

// Inject only the human surfaces for tests; production has no approval flag,
// password argument, environment input or file input.
func executeDoerProtectedSetup(ctx context.Context, cfg config.Config, input app.DoerProtectedSetupInput, acquire func(context.Context) ([]byte, error), confirm func(context.Context, app.DoerProtectedSetupReview) string, emit func(app.DoerProtectedSetupResult) error) error {
	origin, err := url.Parse(cfg.API.Console.Origin)
	if err != nil {
		return errors.New("owning_instance_mismatch")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if !companionSocketSafe(cfg.API.UnixSocket) {
			return nil, errors.New("unsafe owning socket")
		}
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	const base = "/console/loops/doer/setup-native"
	var cookie *http.Cookie
	csrf := ""
	request := func(requestCtx context.Context, path, contentType string, body []byte, out any) error {
		req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "http://"+origin.Host+base+path, bytes.NewReader(body))
		if err != nil {
			return errors.New("protected_setup_unavailable")
		}
		req.Header.Set("Origin", cfg.API.Console.Origin)
		req.Header.Set("Content-Type", contentType)
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		resp, err := client.Do(req)
		if err != nil {
			if path == "/decision" {
				return errors.New("protected_setup_outcome_unknown: read retained draft and exact approval receipts before retry; no automatic retry")
			}
			if requestCtx.Err() != nil {
				return errors.New("protected_setup_" + protectedSetupInputStatus(requestCtx.Err()))
			}
			return errors.New("protected_setup_unavailable")
		}
		defer resp.Body.Close()
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 429 {
			return errors.New("protected_setup_denied")
		}
		if resp.StatusCode != 200 || resp.Header.Get("Location") != "" {
			if path == "/decision" {
				return errors.New("protected_setup_outcome_unknown: inspect exact retained state before retry")
			}
			return errors.New("protected_setup_denied")
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, companionJSONLimit+1))
		defer wipeSecret(data)
		if err != nil || len(data) > companionJSONLimit || !strictCompanionJSON(data, out) {
			return errors.New("protected_setup_response_unverified")
		}
		if path == "/login" {
			for _, c := range resp.Cookies() {
				if c.Name == console.CookieName {
					cookie = c
				}
			}
			if cookie == nil {
				return errors.New("protected_setup_response_unverified")
			}
		}
		return nil
	}
	password, err := acquire(ctx)
	if err != nil {
		wipeSecret(password)
		return errors.New("protected_setup_" + protectedSetupInputStatus(err))
	}
	var login struct {
		CSRF           string `json:"csrf"`
		ConfigIdentity string `json:"config_identity"`
	}
	err = request(ctx, "/login", "application/octet-stream", password, &login)
	wipeSecret(password)
	if err != nil {
		return err
	}
	csrf = login.CSRF
	if csrf == "" {
		return errors.New("protected_setup_response_unverified")
	}
	defer func() {
		// Revocation is bounded independently of a cancelled interaction. No retry
		// of the consequential decision; server TTL also bounds abandoned sessions.
		logoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		var result struct {
			Status string `json:"status"`
		}
		_ = request(logoutCtx, "/logout", "application/json", []byte("{}"), &result)
	}()
	if login.ConfigIdentity != core.Digest(cfg) {
		return errors.New("owning_instance_mismatch")
	}
	raw, _ := json.Marshal(input)
	var review app.DoerProtectedSetupReview
	if err = request(ctx, "/review", "application/json", raw, &review); err != nil {
		return err
	}
	if review.Action != input.Action || review.Draft.ID != input.ID || review.Draft.Version != input.ExpectedVersion || review.Receipt == "" {
		return errors.New("protected_setup_response_unverified")
	}
	if review.Proposal != nil {
		p := review.Proposal
		original, e := core.Canonicalize(p.Original.Charter)
		proposed, e2 := core.Canonicalize(p.Proposed.Charter)
		if e != nil || e2 != nil || original.Digest != p.Original.Digest || proposed.Digest != p.Proposed.Digest || p.DraftID != input.ID || p.DraftVersion != input.ExpectedVersion || p.Expected != review.Draft.Agent {
			return errors.New("protected_setup_response_unverified")
		}
	}
	decision := confirm(ctx, review)
	if decision != "approve" && decision != "reject" {
		return errors.New("protected_setup_" + decision)
	}
	raw, _ = json.Marshal(app.DoerProtectedSetupDecision{Receipt: review.Receipt, Decision: decision})
	var result app.DoerProtectedSetupResult
	if err = request(ctx, "/decision", "application/json", raw, &result); err != nil {
		return err
	}
	if result.Draft.ID != input.ID || !reflect.DeepEqual(result.Draft.Contract, review.Draft.Contract) { // contract has slices; full equality below
		return errors.New("protected_setup_response_unverified")
	}
	if decision == "reject" {
		return errors.New("protected_setup_denied: exact review rejected; retained task unchanged")
	}
	if result.Status != "approved" || !verifyDoerProtectedResult(cfg, review, result) {
		return errors.New("protected_setup_response_unverified")
	}
	return emit(result)
}
