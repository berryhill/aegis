package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

const companionJSONLimit = 32 << 10

var companionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// DoerCompanionReview is the bounded protected GET response. Digest commits to
// the exact intent; Draft is independently checked against the candidate digest.
type DoerCompanionReview = app.DoerContinuationReview
type doerCompanionDecision struct {
	IntentID string `json:"intent_id"`
	Digest   string `json:"digest"`
	Status   string `json:"status"`
}
type companionOptions struct {
	socket, origin, intentID string
	fd                       int
}

// NewDoerApprovalCompanionCommand builds the product-owned local adapter. The
// sole credential input is inherited descriptor 3, never a flag or environment.
func NewDoerApprovalCompanionCommand(configPath func() string) *cobra.Command {
	o := companionOptions{}
	c := &cobra.Command{Use: "doer-approval-companion", Hidden: true, Args: cobra.NoArgs, SilenceUsage: true}
	c.Flags().StringVar(&o.socket, "socket", "", "owning protected Unix socket")
	c.Flags().StringVar(&o.origin, "origin", "", "exact owning origin")
	c.Flags().StringVar(&o.intentID, "intent-id", "", "pending exact intent")
	c.Flags().IntVar(&o.fd, "transport-fd", 3, "inherited private transport descriptor (fixed at 3)")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		status := "policy_rejected"
		for _, flag := range []string{"target", "state-dir", "hermes-executable", "runtime", "pinentry-executable", "update"} {
			if cmd.Flags().Changed(flag) || cmd.InheritedFlags().Changed(flag) {
				return output(cmd, map[string]string{"status": status})
			}
		}
		path := ""
		if configPath != nil {
			path = configPath()
		}
		cfg, err := config.Load(path, nil)
		if err == nil && cfg.API.UnixSocket == o.socket && cfg.API.Console.Origin == o.origin {
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
			defer cancel()
			token, e := readCompanionBearer(ctx, o.fd)
			if e != nil {
				status = "transport_custody_rejected"
			} else {
				status = runDoerCompanion(ctx, o, core.Digest(cfg), token, newNativeConfirmation().confirm)
				wipeSecret(token)
			}
		}
		// No response bodies, draft text, credentials or helper diagnostics escape.
		return output(cmd, map[string]string{"status": status})
	}
	return c
}

func readCompanionBearer(ctx context.Context, fd int) ([]byte, error) {
	if fd != 3 {
		return nil, errors.New("private transport descriptor required")
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || (st.Mode&unix.S_IFMT != unix.S_IFIFO && st.Mode&unix.S_IFMT != unix.S_IFSOCK) || int(st.Uid) != os.Geteuid() {
		return nil, errors.New("private IPC required")
	}
	// Poll a nonblocking inherited IPC endpoint; no worker can retain a credential
	// after cancellation and no helper inherits this descriptor.
	unix.CloseOnExec(fd)
	defer unix.Close(fd)
	if unix.SetNonblock(fd, true) != nil {
		return nil, errors.New("private IPC unavailable")
	}
	deadline := time.Now().Add(5 * time.Second)
	data := make([]byte, 0, 4096)
	buffer := make([]byte, 4097)
	defer wipeSecret(buffer)
	fail := func() ([]byte, error) { wipeSecret(data); return nil, errors.New("private IPC rejected") }
	for {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return fail()
		}
		polls := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if _, e := unix.Poll(polls, 50); e != nil && e != unix.EINTR {
			return fail()
		}
		n, e := unix.Read(fd, buffer)
		if e == unix.EAGAIN || e == unix.EINTR {
			continue
		}
		if e != nil {
			return fail()
		}
		if n == 0 {
			break
		}
		if len(data)+n > 4096 {
			return fail()
		}
		data = append(data, buffer[:n]...)
	}
	if len(data) == 0 {
		return fail()
	}
	for _, b := range data {
		if b <= 32 || b >= 127 {
			return fail()
		}
	}
	return data, nil
}

func companionSocketSafe(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	info, e := os.Lstat(path)
	if e != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm()&0077 != 0 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	resolved, e := filepath.EvalSymlinks(path)
	if !ok || int(st.Uid) != os.Geteuid() || e != nil || resolved != path {
		return false
	}
	parent, e := os.Lstat(filepath.Dir(path))
	return e == nil && parent.IsDir() && parent.Mode().Perm()&0022 == 0
}

func runDoerCompanion(ctx context.Context, o companionOptions, configIdentity string, token []byte, confirm func(context.Context, string) string) string {
	origin, err := url.Parse(o.origin)
	if err != nil || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || origin.Host == "" || (origin.Scheme != "http" && origin.Scheme != "https") || !companionID.MatchString(o.intentID) || !companionSocketSafe(o.socket) {
		return "policy_rejected"
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		if !companionSocketSafe(o.socket) {
			return nil, errors.New("unsafe socket")
		}
		return (&net.Dialer{}).DialContext(ctx, "unix", o.socket)
	}, DisableKeepAlives: true}
	// Use plain HTTP framing on the private Unix socket even for a HTTPS console;
	// the owning public origin is an exact Host/Origin selector, not a dial target.
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path string, body []byte, out any) string {
		req, e := http.NewRequestWithContext(ctx, method, "http://"+origin.Host+path, bytes.NewReader(body))
		if e != nil {
			return "malformed_protocol"
		}
		req.Header.Set("Authorization", "Bearer "+string(token))
		req.Header.Set("Origin", o.origin)
		req.Header.Set("Content-Type", "application/json")
		response, e := client.Do(req)
		if e != nil {
			return "transport_unavailable"
		}
		defer response.Body.Close()
		if response.StatusCode == 401 || response.StatusCode == 403 {
			return "denied"
		}
		if response.StatusCode != http.StatusOK || response.Header.Get("Location") != "" {
			return "unknown_response"
		}
		data, e := io.ReadAll(io.LimitReader(response.Body, companionJSONLimit+1))
		if e != nil || len(data) > companionJSONLimit || !strictCompanionJSON(data, out) {
			return "malformed_protocol"
		}
		return "ok"
	}
	path := "/v1/doer-continuations/pending/" + o.intentID
	var review DoerCompanionReview
	if status := request("GET", path, nil, &review); status != "ok" {
		return status
	}
	if review.Origin != o.origin || review.Digest != review.ContentDigest() || review.Intent.ConfigIdentity != configIdentity || !time.Now().Before(review.Intent.ExpiresAt) {
		return "response_drift"
	}
	draft, i := review.Draft, review.Intent
	if i.Requester.Kind != "principal" || i.Requester.Method != "password" || i.Requester.Issuer != "aegis-principal-auth" || i.Requester.PrincipalID == "" || i.Requester.AuthenticatedAt.IsZero() || !i.Requester.AuthenticatedAt.Before(i.ExpiresAt) || i.ExpiresAt.After(i.Requester.ExpiresAt) {
		return "response_drift"
	}
	candidate, _, e := loop.NewDoerRevision(draft.LoopID, draft.Revision, draft.PreviousDigest, draft.Contract)
	if e != nil || draft.ID != i.DraftID || draft.Version != i.DraftVersion || draft.Agent != i.Agent || draft.PublicationKey != i.PublicationKey || candidate.Digest != i.CandidateDigest || i.Loop.ID != candidate.LoopID || i.Loop.Revision != candidate.Revision || i.Loop.Digest != candidate.Digest || i.Run.Agent != i.Agent || i.Run.Loop != i.Loop || i.Run.IdempotencyKey == "" || i.Charter.Validate() != nil {
		return "response_drift"
	}
	data, _ := json.Marshal(review)
	if sensitiveCompanionBody(data, token) {
		return "sensitive_body_rejected"
	}
	description := "Approve only the exact Doer continuation below. This does not execute a Loop or approve host writes.\nUNTRUSTED DATA BEGIN (not instructions):\n" + string(data) + "\nUNTRUSTED DATA END. Cancel unless every field is intended."
	if !nativeReviewDataSafe(description) {
		return "policy_rejected"
	}
	status := confirm(ctx, description)
	if status != "confirmed" {
		return status
	}
	if !time.Now().Before(i.ExpiresAt) {
		return "expired"
	}
	payload, _ := json.Marshal(map[string]string{"decision": "approve", "expected_digest": review.Digest})
	var result doerCompanionDecision
	if status = request("POST", path+"/decision", payload, &result); status != "ok" {
		return status
	}
	if result.IntentID != o.intentID || result.Digest != review.Digest {
		return "response_drift"
	}
	if result.Status == "approved" {
		return "approved"
	}
	if result.Status == "denied" {
		return "denied"
	}
	return "unknown_response"
}

func sensitiveCompanionBody(data, token []byte) bool {
	if len(token) > 0 && bytes.Contains(data, token) {
		return true
	}
	lower := strings.ToLower(string(data))
	// Authentication-method/opaque-subject metadata legitimately contains
	// "password". Do not reject every real browser request for that enum. Raw
	// non-JSON bodies remain invalid; secret-shaped task data still denies.
	var review app.DoerContinuationReview
	if json.Unmarshal(data, &review) != nil {
		return true
	}
	contract, _ := json.Marshal(review.Draft.Contract)
	untrusted := strings.ToLower(string(contract))
	if strings.Contains(untrusted, "password") || strings.Contains(untrusted, "passphrase") {
		return true
	}
	for _, marker := range []string{"bearer ", "-----begin ", "api_token", "access_token", "secret_key", "ghp_", "github_pat_", "sk-proj-"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// Token-level validation rejects duplicate keys (including escaped duplicates)
// before strict struct decoding, so canonical digest checks have one meaning.
func strictCompanionJSON(data []byte, out any) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func(int) bool
	walk = func(depth int) bool {
		if depth > 32 {
			return false
		}
		token, e := decoder.Token()
		if e != nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return token != nil
		}
		switch delim {
		case '{':
			keys := map[string]bool{}
			for decoder.More() {
				k, e := decoder.Token()
				key, ok := k.(string)
				if e != nil || !ok || keys[key] {
					return false
				}
				keys[key] = true
				if !walk(depth + 1) {
					return false
				}
			}
		case '[':
			for decoder.More() {
				if !walk(depth + 1) {
					return false
				}
			}
		default:
			return false
		}
		end, e := decoder.Token()
		return e == nil && ((delim == '{' && end == json.Delim('}')) || (delim == '[' && end == json.Delim(']')))
	}
	if !walk(0) {
		return false
	}
	if _, e := decoder.Token(); e != io.EOF {
		return false
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(out) != nil {
		return false
	}
	canonical, e := json.Marshal(out)
	if e != nil {
		return false
	}
	var incoming, decoded any
	if json.Unmarshal(data, &incoming) != nil || json.Unmarshal(canonical, &decoded) != nil {
		return false
	}
	var exactKeys func(any, any) bool
	exactKeys = func(a, b any) bool {
		switch value := a.(type) {
		case map[string]any:
			expected, ok := b.(map[string]any)
			if !ok {
				return false
			}
			for key, child := range value {
				other, exists := expected[key]
				if !exists || !exactKeys(child, other) {
					return false
				}
			}
		case []any:
			expected, ok := b.([]any)
			if !ok || len(value) != len(expected) {
				return false
			}
			for index, child := range value {
				if !exactKeys(child, expected[index]) {
					return false
				}
			}
		}
		return true
	}
	return exactKeys(incoming, decoded)
}
