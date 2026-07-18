package broker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxDownstreamResponseBytes = 64 << 10

var githubName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,98}[A-Za-z0-9])?$`)

type ServerConfig struct {
	Socket       string
	AllowedUID   uint32
	AllowedGID   uint32
	MaxBodyBytes int64
	Timeout      time.Duration
	Destinations map[string]string
	Repositories []string
}

type peerContextKey struct{}

type errorEnvelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func Serve(ctx context.Context, authorizer Authorizer, config ServerConfig) error {
	return serve(ctx, authorizer, config, false)
}

func serve(ctx context.Context, authorizer Authorizer, config ServerConfig, allowSameUIDForTest bool) error {
	if authorizer == nil || config.Socket == "" || !filepath.IsAbs(config.Socket) || strings.HasPrefix(config.Socket, "@") || config.MaxBodyBytes < 256 || config.MaxBodyBytes > 1<<20 || config.Timeout <= 0 || config.Timeout > 30*time.Second {
		return errors.New("invalid credential broker configuration")
	}
	parsed := make(map[string]*url.URL, len(config.Destinations))
	for id, raw := range config.Destinations {
		destination, parseErr := url.Parse(raw)
		if parseErr != nil || destination.Host == "" || destination.User != nil || destination.RawQuery != "" || destination.Fragment != "" || (destination.Path != "" && destination.Path != "/") {
			return errors.New("credential broker destination is invalid")
		}
		if destination.Scheme != "https" {
			ip := net.ParseIP(destination.Hostname())
			if destination.Scheme != "http" || ip == nil || !ip.IsLoopback() {
				return errors.New("credential broker destination requires HTTPS; HTTP is loopback-test-only")
			}
		}
		parsed[id] = destination
	}
	if len(parsed) == 0 {
		return errors.New("credential broker has no approved destinations")
	}
	if parsed[GitHubDestination] == nil {
		return errors.New("credential broker requires the github-api destination")
	}
	if len(config.Repositories) == 0 {
		return errors.New("credential broker requires approved repositories")
	}
	for _, repository := range config.Repositories {
		parts := strings.Split(repository, "/")
		if len(parts) != 2 || !githubName.MatchString(parts[0]) || !githubName.MatchString(parts[1]) || strings.Contains(repository, "..") {
			return errors.New("credential broker approved repository is invalid")
		}
	}
	directory := filepath.Dir(config.Socket)
	if err := rejectSymlinkComponents(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return errors.New("credential broker runtime directory must exist and not be group/other writable")
	}
	ownerUID, _, ok := socketOwner(info)
	if !ok || ownerUID != uint32(os.Geteuid()) || (!allowSameUIDForTest && ownerUID == config.AllowedUID) {
		return errors.New("credential broker runtime directory ownership does not separate the runtime identity")
	}
	if existing, statErr := os.Lstat(config.Socket); statErr == nil {
		existingUID, existingGID, ownerOK := socketOwner(existing)
		if existing.Mode()&os.ModeSocket == 0 || !ownerOK || existingUID != uint32(os.Geteuid()) || existingGID != config.AllowedGID || existing.Mode().Perm() != 0660 {
			return errors.New("credential broker path exists with unsafe type, ownership, or mode")
		}
		if err = os.Remove(config.Socket); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	baseListener, err := net.Listen("unix", config.Socket)
	if err != nil {
		return err
	}
	cleanup := func() {
		_ = baseListener.Close()
		if socket, statErr := os.Lstat(config.Socket); statErr == nil && socket.Mode()&os.ModeSocket != 0 {
			_ = os.Remove(config.Socket)
		}
	}
	defer cleanup()
	if err = os.Chmod(config.Socket, 0660); err != nil {
		return err
	}
	if err = os.Chown(config.Socket, os.Geteuid(), int(config.AllowedGID)); err != nil {
		return err
	}
	socketInfo, err := os.Lstat(config.Socket)
	if err != nil || socketInfo.Mode()&os.ModeSocket == 0 || socketInfo.Mode().Perm() != 0660 {
		return errors.New("credential broker socket type or mode verification failed")
	}
	socketUID, socketGID, ok := socketOwner(socketInfo)
	if !ok || socketUID != uint32(os.Geteuid()) || socketGID != config.AllowedGID {
		return errors.New("credential broker socket ownership verification failed")
	}
	authenticated := &peerListener{Listener: baseListener, uid: config.AllowedUID, gid: config.AllowedGID, sem: make(chan struct{}, 32)}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, ResponseHeaderTimeout: config.Timeout, DialContext: (&net.Dialer{Timeout: config.Timeout}).DialContext}
	client := &http.Client{Transport: transport, Timeout: config.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects are disabled") }}
	defer transport.CloseIdleConnections()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/broker/actions/github-get-repository", func(response http.ResponseWriter, request *http.Request) {
		requestID := newRequestID()
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("X-Request-ID", requestID)
		peer, ok := request.Context().Value(peerContextKey{}).(Peer)
		if !ok {
			writeError(response, http.StatusUnauthorized, "unauthenticated", requestID)
			return
		}
		mediaType, _, mediaErr := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if mediaErr != nil || mediaType != "application/json" {
			writeError(response, http.StatusUnsupportedMediaType, "invalid_request", requestID)
			return
		}
		request.Body = http.MaxBytesReader(response, request.Body, config.MaxBodyBytes)
		decoder := json.NewDecoder(request.Body)
		decoder.DisallowUnknownFields()
		var input Request
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			writeError(response, http.StatusBadRequest, "invalid_request", requestID)
			return
		}
		now := time.Now()
		if input.SchemaVersion != 1 || !validRequestID(input.RequestID) || input.Deadline.IsZero() || !now.Before(input.Deadline) || input.Deadline.After(now.Add(config.Timeout)) {
			writeError(response, http.StatusBadRequest, "invalid_request", requestID)
			return
		}
		requestID = input.RequestID
		response.Header().Set("X-Request-ID", requestID)
		if !githubName.MatchString(input.Owner) || !githubName.MatchString(input.Repository) || strings.Contains(input.Owner, "..") || strings.Contains(input.Repository, "..") {
			writeError(response, http.StatusBadRequest, "invalid_request", requestID)
			return
		}
		approved := false
		for _, repository := range config.Repositories {
			if repository == input.Owner+"/"+input.Repository {
				approved = true
				break
			}
		}
		if !approved {
			writeError(response, http.StatusForbidden, "repository_denied", requestID)
			return
		}
		actionContext, cancelAction := context.WithDeadline(request.Context(), input.Deadline)
		defer cancelAction()
		result, executeErr := authorizer.ExecuteBroker(actionContext, peer, input, func(ctx context.Context, secret []byte, grant Grant) (Result, error) {
			destination := parsed[grant.Destination]
			if destination == nil {
				return Result{}, errors.New("destination unavailable")
			}
			target := *destination
			target.RawQuery, target.Fragment = "", ""
			target.Path = "/repos/" + input.Owner + "/" + input.Repository
			downstream, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
			if err != nil {
				return Result{}, errors.New("construct downstream request")
			}
			downstream.Header.Set("Authorization", "Bearer "+string(secret))
			downstream.Header.Set("Accept", "application/json")
			downstream.Header.Set("X-GitHub-Api-Version", "2022-11-28")
			upstreamResponse, err := client.Do(downstream)
			if err != nil {
				return Result{}, errors.New("downstream request failed")
			}
			defer upstreamResponse.Body.Close()
			body, err := io.ReadAll(io.LimitReader(upstreamResponse.Body, maxDownstreamResponseBytes+1))
			mediaType, _, mediaErr := mime.ParseMediaType(upstreamResponse.Header.Get("Content-Type"))
			if err != nil || len(body) > maxDownstreamResponseBytes || upstreamResponse.StatusCode != http.StatusOK || mediaErr != nil || mediaType != "application/json" {
				return Result{}, errors.New("downstream response invalid or oversized")
			}
			var github struct {
				Name          string `json:"name"`
				Private       bool   `json:"private"`
				DefaultBranch string `json:"default_branch"`
				Archived      bool   `json:"archived"`
				Visibility    string `json:"visibility"`
				UpdatedAt     string `json:"updated_at"`
				Owner         struct {
					Login string `json:"login"`
				} `json:"owner"`
			}
			if json.Unmarshal(body, &github) != nil || github.Owner.Login != input.Owner || github.Name != input.Repository || len(github.DefaultBranch) > 255 || (github.Visibility != "public" && github.Visibility != "private" && github.Visibility != "internal") {
				return Result{}, errors.New("downstream response schema mismatch")
			}
			if _, err = time.Parse(time.RFC3339, github.UpdatedAt); err != nil {
				return Result{}, errors.New("downstream response timestamp invalid")
			}
			return Result{StatusCode: upstreamResponse.StatusCode, Outcome: "credential_applied", RequestID: requestID, Repository: Repository{Owner: github.Owner.Login, Name: github.Name, Private: github.Private, DefaultBranch: github.DefaultBranch, Archived: github.Archived, Visibility: github.Visibility, UpdatedAt: github.UpdatedAt}}, nil
		})
		if executeErr != nil {
			status := http.StatusForbidden
			code := "denied"
			if errors.Is(executeErr, ErrDownstream) {
				status, code = http.StatusBadGateway, "downstream_failed"
				if errors.Is(actionContext.Err(), context.DeadlineExceeded) || errors.Is(executeErr, context.DeadlineExceeded) {
					status, code = http.StatusGatewayTimeout, "downstream_timeout"
				}
			}
			writeError(response, status, code, requestID)
			return
		}
		response.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(response).Encode(result)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: config.Timeout, WriteTimeout: config.Timeout, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10, ConnContext: func(ctx context.Context, connection net.Conn) context.Context {
		if authenticated, ok := connection.(*authenticatedConn); ok {
			return context.WithValue(ctx, peerContextKey{}, authenticated.peer)
		}
		return ctx
	}}
	done := make(chan error, 1)
	go func() { done <- server.Serve(authenticated) }()
	select {
	case err = <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), config.Timeout)
		defer cancel()
		_ = server.Shutdown(shutdown)
		err = <-done
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newRequestID() string {
	value := make([]byte, 12)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}

func validRequestID(value string) bool {
	if len(value) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func rejectSymlinkComponents(path string) error {
	clean := filepath.Clean(path)
	current := string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(clean, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return errors.New("credential broker socket directory path is unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("credential broker socket directory path contains a symlink")
		}
	}
	return nil
}

func writeError(response http.ResponseWriter, status int, code, requestID string) {
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(errorEnvelope{Code: code, Message: http.StatusText(status), RequestID: requestID})
}
