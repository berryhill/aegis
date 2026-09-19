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
	"net/http/httptrace"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/spf13/cobra"
)

// The console URL is an instance selector, never a destination for transport
// credentials. All requests use the configured protected Unix transport and
// the server's existing peer authentication. No store constructor is called.
func runFleetOnline(cmd *cobra.Command, args []string, options *rootOptions) error {
	return runFleetOnlineWithTimeouts(cmd, args, options, 30*time.Second, 6*time.Minute)
}

// Execution waits cover the bounded five-minute Hermes attempt plus disposition.
// Control reads retain their short deadline; caller cancellation always wins.
func runFleetOnlineWithTimeouts(cmd *cobra.Command, args []string, options *rootOptions, controlTimeout, executionTimeout time.Duration) error {
	operation := ""
	if cmd.Parent() != nil {
		operation = cmd.Parent().Name() + " " + cmd.Name()
	}
	method, path := http.MethodGet, ""
	var input any
	switch operation {
	case "charter import", "charter validate":
		file, err := os.Open(args[0])
		if err != nil {
			return usage(err)
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		if err != nil || len(data) > 1<<20 {
			return usage(errors.New("charter input unreadable or exceeds 1 MiB"))
		}
		// Charter API consumes source bytes (JSON or YAML), not a JSON string.
		method, path, input = http.MethodPost, "/v1/charters/"+cmd.Name(), data
	case "charter show":
		revision, err := exactRevision(args)
		if err != nil {
			return usage(err)
		}
		path = "/v1/charters/" + url.PathEscape(args[0]) + fmt.Sprintf("/%d", revision)
	case "plan preview", "session preview":
		revision, _ := cmd.Flags().GetUint64("revision")
		environment, _ := cmd.Flags().GetString("environment")
		proposal := map[string]any{"agent": args[0], "revision": revision, "environment": coreEnvironment(environment)}
		if operation == "session preview" {
			stanza, _ := cmd.Flags().GetString("stanza")
			proposal["stanza"] = stanza
		}
		method, path, input = http.MethodPost, "/v1/"+cmd.Parent().Name()+"s/preview", proposal
	case "plan show":
		path = "/v1/plans/" + url.PathEscape(args[0])
	case "approval request":
		ttl, _ := cmd.Flags().GetDuration("ttl")
		method, path, input = http.MethodPost, "/v1/approvals", map[string]any{"plan_id": args[0], "ttl": ttl.String()}
	case "approval approve", "approval reject":
		method, path, input = http.MethodPost, "/v1/approvals/"+url.PathEscape(args[0])+"/decision", map[string]bool{"approve": operation == "approval approve"}
	case "approval show":
		path = "/v1/approvals/" + url.PathEscape(args[0])
	case "aegis provision":
		method, path, input = http.MethodPost, "/v1/provision", map[string]string{"plan_id": args[0], "approval_id": args[1]}
	case "session start":
		method, path, input = http.MethodPost, "/v1/sessions/start", map[string]string{"mandate_id": args[0]}
	case "session show", "session authority":
		path = "/v1/sessions/" + url.PathEscape(args[0])
		if operation == "session authority" {
			path += "/authority"
		}
	case "session list":
		path = "/v1/sessions"
	case "agents approve-charter":
		var proposal app.ApproveAgentCharterInput
		if err := decodeJSONFile(args[1], &proposal); err != nil {
			return usage(err)
		}
		if proposal.Expected.ID != args[0] || proposal.Charter.ID != args[0] {
			return usage(errors.New("agent path and exact references must match"))
		}
		method, path, input = http.MethodPut, "/v1/agents/"+url.PathEscape(args[0])+"/charter", proposal
	case "agents list":
		path = "/v1/agents"
	case "agents show":
		revision, err := exactRevision(args)
		if err != nil {
			return usage(err)
		}
		path = "/v1/agents/" + url.PathEscape(args[0]) + fmt.Sprintf("?revision=%d", revision)
	case "agents history":
		path = "/v1/agents/" + url.PathEscape(args[0]) + "/revisions"
	case "loops queue":
		var proposal app.QueueLoopInput
		if err := decodeJSONFile(args[0], &proposal); err != nil {
			return usage(err)
		}
		method, path, input = http.MethodPost, "/v1/loops/queue", proposal
	case "loops list":
		path = "/v1/loops"
	case "loops show":
		revision, err := exactRevision(args)
		if err != nil {
			return usage(err)
		}
		path = "/v1/loops/" + url.PathEscape(args[0]) + fmt.Sprintf("/%d", revision)
	case "loops publish", "loops validate":
		var proposal app.PublishLoopInput
		if err := decodeJSONFile(args[0], &proposal); err != nil {
			return usage(err)
		}
		if proposal.AgentID == "" {
			return errors.New("workspace_agent_selection_required: select an existing enabled Agent with the owner and set agent_id; no builder is inferred from its name")
		}
		if proposal.Authority.ID != "" || proposal.Authority.Digest != "" || proposal.Publisher.ID != "" || proposal.Publisher.Digest != "" || proposal.Publisher.Revision != 0 {
			return errors.New("workspace_authority_must_be_server_derived: omit authority and publisher")
		}
		method, path, input = http.MethodPost, "/v1/loops", proposal
		if operation == "loops validate" {
			path += "/validate"
		}
	case "loops activate", "loops retire":
		var proposal app.SetLoopLifecycleInput
		if err := decodeJSONFile(args[1], &proposal); err != nil {
			return usage(err)
		}
		if proposal.AgentID == "" || proposal.Authority.ID != "" || proposal.Authority.Digest != "" || proposal.Publisher.ID != "" || proposal.Publisher.Digest != "" || proposal.Publisher.Revision != 0 {
			return usage(errors.New("workspace lifecycle requires agent_id and server-derived authority and publisher"))
		}
		if proposal.Loop.ID != args[0] {
			return usage(errors.New("loop path and body must match"))
		}
		proposal.State = "active"
		if operation == "loops retire" {
			proposal.State = "retired"
		}
		method, path, input = http.MethodPut, "/v1/loops/"+url.PathEscape(args[0])+"/lifecycle", proposal
	case "graphs list":
		path = "/v1/graphs"
	case "graphs show":
		revision, err := exactRevision(args)
		if err != nil {
			return usage(err)
		}
		path = "/v1/graphs/" + url.PathEscape(args[0]) + fmt.Sprintf("/%d", revision)
	case "graphs publish":
		var proposal app.PublishGraphInput
		if err := decodeJSONFile(args[0], &proposal); err != nil {
			return usage(err)
		}
		if proposal.AgentID == "" || proposal.Authority.ID != "" || proposal.Authority.Digest != "" {
			return usage(errors.New("workspace publication requires agent_id and server-derived authority"))
		}
		method, path, input = http.MethodPost, "/v1/graphs", proposal
	case "graphs submit":
		// Never turn a requested shape-only check into a mutation.
		if check, _ := cmd.Flags().GetBool("check"); check {
			return usage(errors.New("--target cannot be combined with --check; omit --target for offline request-shape validation"))
		}
		var proposal app.SubmitGraphInput
		if err := decodeJSONFile(args[0], &proposal); err != nil {
			return usage(err)
		}
		if proposal.Workspace != nil {
			return usage(errors.New("workspace authority must be server-derived"))
		}
		method, path, input = http.MethodPost, "/v1/queue", proposal
	case "queue preparations":
		path = "/v1/preparations"
	case "queue list":
		path = "/v1/queue"
	case "queue show":
		path = "/v1/queue/" + url.PathEscape(args[0])
	case "queue process":
		var proposal app.ProcessQueueItemInput
		if err := decodeJSONFile(args[0], &proposal); err != nil {
			return usage(err)
		}
		if proposal.QueueItemID == "" {
			return usage(errors.New("queue_item_id is required"))
		}
		method, path, input = http.MethodPost, "/v1/queue/"+url.PathEscape(proposal.QueueItemID)+"/process", proposal
	case "queue bind-runtime":
		var proposal app.BindQueueRuntimeInput
		if err := decodeJSONFile(args[0], &proposal); err != nil {
			return usage(err)
		}
		if proposal.QueueItemID == "" || proposal.AgentID == "" {
			return usage(errors.New("queue_item_id and agent_id are required"))
		}
		method, path, input = http.MethodPost, "/v1/queue/"+url.PathEscape(proposal.QueueItemID)+"/bind-runtime", proposal
	default:
		return usage(errors.New("online_operation_unsupported: operation is not in the owning-service transport allowlist"))
	}
	for _, flag := range []string{"state-dir", "hermes-executable", "runtime", "pinentry-executable", "update"} {
		if cmd.Flags().Changed(flag) || cmd.InheritedFlags().Changed(flag) {
			return usage(fmt.Errorf("--target cannot be combined with --%s", flag))
		}
	}
	cfg, err := config.Load(options.configFile, nil)
	if err != nil {
		return usage(err)
	}
	target, err := url.Parse(options.target)
	if err != nil || target.User != nil || target.RawQuery != "" || (target.Path != "" && target.Path != "/" && target.Path != "/console" && !strings.HasPrefix(target.Path, "/console/")) || target.Scheme+"://"+target.Host != cfg.API.Console.Origin {
		return errors.New("owning_instance_mismatch: select the existing owning service configuration with --config; target must match api.console.origin; do not initialize another store")
	}
	if cfg.API.UnixSocket == "" || cfg.API.Token == "" {
		return errors.New("owning_instance_unavailable: owning configuration requires protected Unix transport; no HTTP bearer or local-store fallback")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", cfg.API.UnixSocket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path string, input, output any) error {
		var body io.Reader
		if input != nil {
			data, raw := input.([]byte)
			if !raw {
				var err error
				data, err = json.Marshal(input)
				if err != nil {
					return err
				}
			}
			body = bytes.NewReader(data)
		}
		timeout := controlTimeout
		processing := method != http.MethodGet
		if processing && (strings.HasSuffix(path, "/process") || path == "/v1/loops/queue" || path == "/v1/provision" || path == "/v1/sessions/start") {
			timeout = executionTimeout
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		var dispatched atomic.Bool
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { dispatched.Store(true) }})
		unknown := func() error {
			return fmt.Errorf("execution_outcome_unknown: %s %s may have executed; read exact owning-service records before any retry; no automatic retry", method, path)
		}
		req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+cfg.API.Token)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			if processing && dispatched.Load() {
				return unknown()
			}
			return errors.New("owning_instance_unavailable: cannot reach configured protected Unix API; no local-store fallback")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
			if processing && response.StatusCode >= 500 {
				return unknown()
			}
			var denial struct {
				Decision struct {
					Reason string `json:"reason"`
				} `json:"decision"`
			}
			_ = json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&denial)
			// Only fixed public diagnostic codes may cross this boundary. Never echo
			// arbitrary remote error text or trusted-input metadata.
			switch denial.Decision.Reason {
			case "provisioning_receipt_missing", "provisioning_receipt_unavailable":
				return fmt.Errorf("owning_service_denied: %s %s returned HTTP %d (%s)", method, path, response.StatusCode, denial.Decision.Reason)
			}
			return fmt.Errorf("owning_service_denied: %s %s returned HTTP %d", method, path, response.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
		if err != nil || len(data) > 4<<20 {
			if processing {
				return unknown()
			}
			return errors.New("invalid owning service response: unreadable or oversized body")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(output); err != nil {
			if processing {
				return unknown()
			}
			return fmt.Errorf("invalid owning service response: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			if processing {
				return unknown()
			}
			return errors.New("invalid owning service response: trailing JSON or data")
		}
		return nil
	}
	var actual config.Config
	if err := request(http.MethodGet, "/v1/config", nil, &actual); err != nil {
		return err
	}
	if actual.API.Console.Origin != cfg.API.Console.Origin || actual.StateDir != cfg.StateDir || actual.Principal.ID != cfg.Principal.ID || actual.API.UnixSocket != cfg.API.UnixSocket {
		return errors.New("owning_instance_mismatch: running service and selected configuration disagree; no action performed")
	}
	var result json.RawMessage
	if err := request(method, path, input, &result); err != nil {
		return err
	}
	return output(cmd, result)
}
