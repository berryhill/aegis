package command

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
)

// These are transport regressions, not service or installed-API acceptance.
func TestFleetOnlineLifecycleAdapters(t *testing.T) {
	root, err := os.MkdirTemp(os.Getenv("TMPDIR"), "f-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	cfg := config.Defaults()
	cfg.StateDir = filepath.Join(root, "no-store")
	cfg.API.UnixSocket = filepath.Join(root, "s")
	cfg.API.Token = "synthetic-transport-token"
	cfg.Principal.UID, cfg.Principal.User = "1000", "synthetic"
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(http.StatusOK)
	var gotMethod, gotPath string
	var gotBody []byte
	listener, err := net.Listen("unix", cfg.API.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+cfg.API.Token {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v1/config" {
			json.NewEncoder(w).Encode(config.Redacted(cfg))
			return
		}
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		gotBody, _ = io.ReadAll(r.Body)
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		io.WriteString(w, `{}`)
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(root, "config.json")
	if err := os.WriteFile(cfgFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	inputFile := filepath.Join(root, "input.json")
	run := func(group, verb, id, body string) error {
		t.Helper()
		if err := os.WriteFile(inputFile, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{"--config", cfgFile, "--target", cfg.API.Console.Origin, group, verb}
		if group == "agents" {
			args = append(args, id)
		}
		args = append(args, inputFile)
		cmd := NewRoot(Dependencies{Out: io.Discard, Err: io.Discard})
		cmd.SetArgs(args)
		return cmd.ExecuteContext(context.Background())
	}
	agentBody := `{"expected":{"schema_version":"v1","id":"builder","revision":7,"digest":"exact-agent-digest"},"lifecycle":"retired"}`
	terminalBody := `{"authority":{"id":"authority","digest":"exact-authority-digest"},"queue_item_id":"item","cancellation_id":"cancel-1","transition_id":"transition-1","reason_code":"operator_cancelled","agent_id":"builder"}`
	retryBody := `{"authority":{"id":"authority","digest":"exact-authority-digest"},"queue_item_id":"item","retry_id":"retry-1","transition_id":"transition-1","backoff":123,"reclaimed":true,"reason_code":"lease_reclaimed","agent_id":"builder"}`
	for _, tc := range []struct{ group, verb, body, path, state string }{
		{"agents", "enable", agentBody, "/v1/agents/builder/lifecycle", "enabled"},
		{"agents", "disable", agentBody, "/v1/agents/builder/lifecycle", "disabled"},
		{"agents", "retire", agentBody, "/v1/agents/builder/lifecycle", "retired"},
		{"queue", "cancel", terminalBody, "/v1/queue/item/cancel", ""},
		{"queue", "revoke", terminalBody, "/v1/queue/item/revoke", ""},
		{"queue", "retry", retryBody, "/v1/queue/item/retry", ""},
	} {
		t.Run(tc.group+"_"+tc.verb, func(t *testing.T) {
			before := calls.Load()
			if err := run(tc.group, tc.verb, "builder", tc.body); err != nil {
				t.Fatal(err)
			}
			wantMethod := http.MethodPost
			var want any
			switch tc.group + " " + tc.verb {
			case "agents enable", "agents disable", "agents retire":
				var value app.SetAgentLifecycleInput
				if err := json.Unmarshal([]byte(tc.body), &value); err != nil {
					t.Fatal(err)
				}
				switch tc.state {
				case "enabled":
					value.Lifecycle = "enabled"
				case "disabled":
					value.Lifecycle = "disabled"
				case "retired":
					value.Lifecycle = "retired"
				}
				want = value
				wantMethod = http.MethodPut
			case "queue retry":
				var value app.RetryQueueItemInput
				if err := json.Unmarshal([]byte(tc.body), &value); err != nil {
					t.Fatal(err)
				}
				want = value
			default:
				var value app.TerminalQueueItemInput
				if err := json.Unmarshal([]byte(tc.body), &value); err != nil {
					t.Fatal(err)
				}
				want = value
			}
			expected, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			var a, b any
			if err := json.Unmarshal(expected, &a); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(gotBody, &b); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != before+1 || gotMethod != wantMethod || gotPath != tc.path || !reflect.DeepEqual(a, b) {
				t.Fatalf("mapping: method=%s path=%s body=%s want %s %s %s", gotMethod, gotPath, gotBody, wantMethod, tc.path, expected)
			}
			for _, bad := range []struct{ body, part string }{
				{`{`, "unexpected EOF"},
				{strings.TrimSuffix(tc.body, "}") + `,"unknown":true}`, "unknown field"},
				{tc.body + ` {}`, "trailing JSON"},
				{strings.Replace(tc.body, `"revision":7`, `"revision":"bad"`, 1), "cannot unmarshal"},
			} {
				if bad.body == tc.body {
					continue
				}
				before = calls.Load()
				err := run(tc.group, tc.verb, "builder", bad.body)
				if err == nil || !strings.Contains(err.Error(), bad.part) || calls.Load() != before {
					t.Fatalf("strict refusal: %v calls=%d", err, calls.Load()-before)
				}
			}
			before = calls.Load()
			badBody := strings.Replace(tc.body, `"queue_item_id":"item"`, `"queue_item_id":""`, 1)
			id := "other"
			if tc.group == "agents" {
				badBody = tc.body
			}
			err = run(tc.group, tc.verb, id, badBody)
			if err == nil || calls.Load() != before {
				t.Fatalf("path/identity mismatch admitted: %v", err)
			}
			for _, code := range []int{http.StatusForbidden, http.StatusConflict, http.StatusInternalServerError} {
				status.Store(int32(code))
				before = calls.Load()
				err = run(tc.group, tc.verb, "builder", tc.body)
				part := "owning_service_denied"
				if code == 500 {
					part = "execution_outcome_unknown"
				}
				if err == nil || !strings.Contains(err.Error(), part) || calls.Load() != before+1 {
					t.Fatalf("denial/uncertainty or automatic retry: %v calls=%d", err, calls.Load()-before)
				}
			}
			status.Store(http.StatusOK)
		})
	}
	// Unsupported online commands stop before transport/store access.
	for _, verb := range []string{"expire", "exhaust"} {
		before := calls.Load()
		err := run("queue", verb, "", terminalBody)
		if err == nil || !strings.Contains(err.Error(), "online_operation_unsupported") || calls.Load() != before {
			t.Fatalf("unsupported %s: %v calls=%d", verb, err, calls.Load()-before)
		}
	}
	if _, err := os.Stat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatalf("alternate store created: %v", err)
	}
}
