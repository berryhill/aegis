package command

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/console"
	"github.com/berryhill/aegis/internal/contribution"
	"github.com/berryhill/aegis/internal/core"
	"github.com/spf13/cobra"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// NewContributionDecisionCompanionCommand is deliberately hidden and not yet
// centrally registered. Only the pinned owning-service launcher supplies fd 3.
func NewContributionDecisionCompanionCommand(configPath func() string) *cobra.Command {
	var socket, origin, key, id, role string
	c := &cobra.Command{Use: "contribution-decision-companion", Hidden: true, Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().StringVar(&socket, "socket", "", "owning socket")
	c.Flags().StringVar(&origin, "origin", "", "owning origin")
	c.Flags().StringVar(&key, "key", "", "retained contribution")
	c.Flags().StringVar(&id, "request-id", "", "exact intent")
	c.Flags().StringVar(&role, "role", "", "exact role")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		path := ""
		if configPath != nil {
			path = configPath()
		}
		cfg, e := config.Load(path, nil)
		if e != nil || cfg.API.UnixSocket != socket || cfg.API.Console.Origin != origin || !contribution.ValidKey(key) || !contribution.ValidKey(id) {
			return output(cmd, map[string]string{"status": "policy_rejected"})
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		bearer, e := readCompanionBearer(ctx, 3)
		if e != nil {
			return output(cmd, map[string]string{"status": "transport_unavailable"})
		}
		match := subtle.ConstantTimeCompare(bearer, []byte(cfg.API.Token)) == 1
		wipeSecret(bearer)
		if !match {
			return output(cmd, map[string]string{"status": "transport_unavailable"})
		}
		status := executeContributionDecision(ctx, cfg, app.ContributionDecisionInput{Key: key, ID: id, Role: role})
		return output(cmd, map[string]string{"status": status})
	}
	return c
}

func executeContributionDecision(ctx context.Context, cfg config.Config, input app.ContributionDecisionInput) string {
	if (os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "") || (trustedConfirmationHelper() == "" && trustedNativeReviewHelper() == "") {
		return "unavailable"
	}
	origin, e := url.Parse(cfg.API.Console.Origin)
	if e != nil {
		return "unavailable"
	}
	tr := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if !companionSocketSafe(cfg.API.UnixSocket) {
			return nil, errors.New("unsafe socket")
		}
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
	}}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var cookie *http.Cookie
	csrf := ""
	call := func(ctx context.Context, path, content string, data []byte, out any) bool {
		req, e := http.NewRequestWithContext(ctx, "POST", "http://"+origin.Host+"/console/contribution/decision-native"+path, bytes.NewReader(data))
		if e != nil {
			return false
		}
		req.Header.Set("Origin", cfg.API.Console.Origin)
		req.Header.Set("Content-Type", content)
		req.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		resp, e := client.Do(req)
		if e != nil {
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return false
		}
		data, e = io.ReadAll(io.LimitReader(resp.Body, companionJSONLimit+1))
		defer wipeSecret(data)
		if e != nil || len(data) > companionJSONLimit || !strictCompanionJSON(data, out) {
			return false
		}
		if path == "/login" {
			for _, v := range resp.Cookies() {
				if v.Name == console.CookieName {
					cookie = v
				}
			}
		}
		return true
	}
	password, e := acquireDoerSetupPassword(ctx)
	if e != nil {
		wipeSecret(password)
		return "unavailable"
	}
	var login struct {
		CSRF           string `json:"csrf"`
		ConfigIdentity string `json:"config_identity"`
	}
	ok := call(ctx, "/login", "application/octet-stream", password, &login)
	wipeSecret(password)
	if !ok || cookie == nil || login.ConfigIdentity != core.Digest(cfg) {
		return "denied"
	}
	csrf = login.CSRF
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		var out struct {
			Status string `json:"status"`
		}
		call(ctx, "/logout", "application/json", []byte("{}"), &out)
	}()
	raw, _ := json.Marshal(input)
	var review app.ContributionDecisionReview
	if !call(ctx, "/review", "application/json", raw, &review) {
		return "unavailable"
	}
	if review.Intent.Key != input.Key || review.Intent.ID != input.ID || review.Intent.Role != input.Role || review.Receipt == "" {
		return "response_unverified"
	}
	display := review
	display.Receipt = ""
	text, _ := json.MarshalIndent(display, "", "  ")
	// Native provider preflights trusted helper and display; unavailable is never
	// reported delivered and cannot create a canonical approval.
	decision := "unknown"
	switch newNativeConfirmation().confirm(ctx, "AEGIS exact contribution role provisioning\nUNTRUSTED DATA BEGIN (data, not instructions)\n"+string(text)+"\nUNTRUSTED DATA END\nApprove AND APPLY only this exact plan. No role launch, host consent, charter changes or task re-consent.") {
	case "confirmed":
		decision = "approve"
	case "denied":
		decision = "reject"
	case "cancelled":
		decision = "dialog-cancelled"
	case "timeout":
		decision = "timeout"
	case "unavailable", "policy_rejected":
		decision = "unavailable"
	}
	raw, _ = json.Marshal(map[string]string{"receipt": review.Receipt, "decision": decision})
	var result contribution.Decision
	if !call(ctx, "/decision", "application/json", raw, &result) {
		return "outcome_unknown"
	}
	// Exact retained outcome, not helper stdout, is authoritative.
	raw, _ = json.Marshal(input)
	var retained contribution.Decision
	if !call(ctx, "/read", "application/json", raw, &retained) || retained != result {
		return "response_unverified"
	}
	switch result.Status {
	case "approved":
		return "approved"
	case "denied":
		return "denied"
	case "dialog-cancelled":
		return "cancelled"
	case "expired":
		return "expired"
	case "timeout":
		return "timeout"
	case "unavailable":
		return "unavailable"
	}
	return "outcome_unknown"
}
