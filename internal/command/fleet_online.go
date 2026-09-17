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
	"strings"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/spf13/cobra"
)

// The console URL is an instance selector, never a destination for transport
// credentials. All requests use the configured protected Unix transport and
// the server's existing peer authentication. No store constructor is called.
func runFleetOnline(cmd *cobra.Command, args []string, options *rootOptions) error {
	operation := ""
	if cmd.Parent() != nil {
		operation = cmd.Parent().Name() + " " + cmd.Name()
	}
	method, path := http.MethodGet, ""
	var input any
	switch operation {
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
	default:
		return usage(errors.New("online_operation_unsupported: --target supports agents list/show/history and loops list/show/validate/publish only; it never activates or executes work"))
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
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(method, path string, input, output any) error {
		var body io.Reader
		if input != nil {
			data, err := json.Marshal(input)
			if err != nil {
				return err
			}
			body = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(cmd.Context(), method, "http://unix"+path, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+cfg.API.Token)
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			return errors.New("owning_instance_unavailable: cannot reach configured protected Unix API; no local-store fallback")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
			return fmt.Errorf("owning_service_denied: %s %s returned HTTP %d", method, path, response.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
		if err != nil || len(data) > 4<<20 {
			return errors.New("invalid owning service response: unreadable or oversized body")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(output); err != nil {
			return fmt.Errorf("invalid owning service response: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
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
