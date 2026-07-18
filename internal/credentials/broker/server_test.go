//go:build linux

package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeAuthorizer struct {
	calls  atomic.Int32
	secret []byte
	err    error
}

func (authorizer *fakeAuthorizer) ExecuteBroker(ctx context.Context, _ Peer, request Request, execute Executor) (Result, error) {
	authorizer.calls.Add(1)
	if authorizer.err != nil {
		return Result{}, authorizer.err
	}
	return execute(ctx, authorizer.secret, Grant{Destination: GitHubDestination})
}

func unixClient(socket string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
}

func validServerRequest() Request {
	return Request{SchemaVersion: 1, RequestID: "42424242424242424242424242424242", Deadline: time.Now().Add(500 * time.Millisecond), Capability: string(bytes.Repeat([]byte("a"), 64)), Owner: "approved-owner", Repository: "approved-repository"}
}

func startTestBroker(t *testing.T, authorizer Authorizer, destination string, uid, gid uint32) (string, context.CancelFunc) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(directory, "broker.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, authorizer, ServerConfig{Socket: socket, AllowedUID: uid, AllowedGID: gid, MaxBodyBytes: 1024, Timeout: time.Second, Destinations: map[string]string{GitHubDestination: destination}, Repositories: []string{"approved-owner/approved-repository"}}, true)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if info, err := os.Lstat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			ownerUID, ownerGID, ok := socketOwner(info)
			if !ok || info.Mode().Perm() != 0660 || ownerUID != uint32(os.Geteuid()) || ownerGID != gid {
				cancel()
				t.Fatalf("unsafe socket metadata mode=%#o uid=%d gid=%d", info.Mode().Perm(), ownerUID, ownerGID)
			}
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("broker socket did not become ready")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return socket, func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("broker shutdown: %v", err)
		}
		if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("broker socket remained after shutdown: %v", err)
		}
	}
}

func TestSocketPathSecurityFailsClosed(t *testing.T) {
	authorizer := &fakeAuthorizer{secret: []byte("unused")}
	baseConfig := func(socket string) ServerConfig {
		return ServerConfig{Socket: socket, AllowedUID: uint32(os.Getuid()), AllowedGID: uint32(os.Getgid()), MaxBodyBytes: 1024, Timeout: time.Second, Destinations: map[string]string{GitHubDestination: "http://127.0.0.1:1"}, Repositories: []string{"approved-owner/approved-repository"}}
	}
	t.Run("abstract socket", func(t *testing.T) {
		if err := serve(context.Background(), authorizer, baseConfig("@aegis-test"), true); err == nil {
			t.Fatal("abstract socket was accepted")
		}
	})
	t.Run("same runtime identity", func(t *testing.T) {
		directory := t.TempDir()
		if err := serve(context.Background(), authorizer, baseConfig(filepath.Join(directory, "broker.sock")), false); err == nil {
			t.Fatal("same service/runtime identity was accepted")
		}
	})
	t.Run("writable runtime directory", func(t *testing.T) {
		directory := t.TempDir()
		if err := os.Chmod(directory, 0770); err != nil {
			t.Fatal(err)
		}
		if err := serve(context.Background(), authorizer, baseConfig(filepath.Join(directory, "broker.sock")), true); err == nil {
			t.Fatal("group-writable runtime directory was accepted")
		}
	})
	t.Run("symlink component", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "target")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := serve(context.Background(), authorizer, baseConfig(filepath.Join(link, "broker.sock")), true); err == nil {
			t.Fatal("symlinked runtime directory was accepted")
		}
	})
	t.Run("non-socket substitution", func(t *testing.T) {
		directory := t.TempDir()
		path := filepath.Join(directory, "broker.sock")
		if err := os.WriteFile(path, []byte("do-not-delete"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := serve(context.Background(), authorizer, baseConfig(path), true); err == nil {
			t.Fatal("non-socket path substitution was accepted")
		}
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != "do-not-delete" {
			t.Fatalf("substituted file was altered: %q %v", contents, err)
		}
	})
}

func TestPeerRejectedBeforeActionBody(t *testing.T) {
	authorizer := &fakeAuthorizer{secret: []byte("unused")}
	socket, stop := startTestBroker(t, authorizer, "http://127.0.0.1:1", uint32(os.Getuid()+1), uint32(os.Getgid()))
	defer stop()
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	done := make(chan error, 1)
	go func() {
		_, writeErr := io.WriteString(connection, "POST /v1/broker/actions/github-get-repository HTTP/1.1\r\nHost: unix\r\nContent-Type: application/json\r\nContent-Length: 1000000\r\n\r\n")
		if writeErr == nil {
			var one [1]byte
			_, writeErr = connection.Read(one[:])
		}
		done <- writeErr
	}()
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("unauthorized peer received an HTTP response")
		}
	case <-time.After(time.Second):
		t.Fatal("unauthorized peer was not rejected before waiting for body")
	}
	if authorizer.calls.Load() != 0 {
		t.Fatal("action body reached authorizer for unauthorized peer")
	}
}

func TestStrictBrokerDecodeAndCredentialApplication(t *testing.T) {
	const canary = "broker-canary-secret"
	received := make(chan string, 1)
	downstream := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		received <- request.Header.Get("Authorization")
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"name":"approved-repository","owner":{"login":"approved-owner"},"private":true,"default_branch":"main","archived":false,"visibility":"private","updated_at":"2026-07-17T00:00:00Z","private_extra":"must-not-return"}`))
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go downstream.Serve(listener)
	defer downstream.Shutdown(context.Background())
	authorizer := &fakeAuthorizer{secret: []byte(canary)}
	socket, stop := startTestBroker(t, authorizer, "http://"+listener.Addr().String(), uint32(os.Getuid()), uint32(os.Getgid()))
	defer stop()
	client := unixClient(socket)
	valid := validServerRequest()
	body, _ := json.Marshal(valid)
	request, _ := http.NewRequest(http.MethodPost, "http://unix/v1/broker/actions/github-get-repository", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	resultBody, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || bytes.Contains(resultBody, []byte(canary)) || bytes.Contains(resultBody, []byte("must-not-return")) {
		t.Fatalf("unsafe broker response status=%d body=%s", response.StatusCode, resultBody)
	}
	if got := <-received; got != "Bearer "+canary {
		t.Fatalf("downstream authentication=%q", got)
	}
	for _, malformed := range [][]byte{[]byte(`{"owner":"x","repository":"x","profile":"authority"}`), append(body, []byte(` {}`)...), []byte(`{`), []byte(`{"capability":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","owner":"../escape","repository":"x"}`), bytes.Repeat([]byte("x"), 2048)} {
		request, _ = http.NewRequest(http.MethodPost, "http://unix/v1/broker/actions/github-get-repository", bytes.NewReader(malformed))
		request.Header.Set("Content-Type", "application/json")
		response, err = client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest && response.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("malformed status=%d", response.StatusCode)
		}
	}
}

func TestDownstreamTimeoutAndRedirectFailClosed(t *testing.T) {
	blocked := &fakeAuthorizer{secret: []byte("canary")}
	downstream := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) { time.Sleep(300 * time.Millisecond) })}
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	go downstream.Serve(listener)
	defer downstream.Shutdown(context.Background())
	directory := t.TempDir()
	_ = os.Chmod(directory, 0700)
	socket := filepath.Join(directory, "broker.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, blocked, ServerConfig{Socket: socket, AllowedUID: uint32(os.Getuid()), AllowedGID: uint32(os.Getgid()), MaxBodyBytes: 1024, Timeout: 50 * time.Millisecond, Destinations: map[string]string{GitHubDestination: "http://" + listener.Addr().String()}, Repositories: []string{"approved-owner/approved-repository"}}, true)
	}()
	for i := 0; i < 200; i++ {
		if _, err := os.Lstat(socket); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	timeoutRequest := validServerRequest()
	timeoutRequest.Deadline = time.Now().Add(25 * time.Millisecond)
	body, _ := json.Marshal(timeoutRequest)
	request, _ := http.NewRequest(http.MethodPost, "http://unix/v1/broker/actions/github-get-repository", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := unixClient(socket).Do(request)
	if err == nil {
		response.Body.Close()
	}
	if err == nil && response.StatusCode != http.StatusForbidden {
		t.Fatalf("timeout status=%d", response.StatusCode)
	}
	cancel()
	if err = <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestDownstreamResponseAndRedirectFailuresAreSanitized(t *testing.T) {
	redirectCalls := atomic.Int32{}
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectCalls.Add(1) }))
	defer redirectTarget.Close()
	tests := []struct {
		name    string
		handler http.Handler
	}{
		{"redirect", http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Location", redirectTarget.URL)
			response.WriteHeader(http.StatusFound)
		})},
		{"oversized", http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write(bytes.Repeat([]byte("x"), maxDownstreamResponseBytes+1))
		})},
		{"wrong content type", http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "text/plain")
			_, _ = response.Write([]byte("secret downstream error"))
		})},
		{"malformed json", http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte("{"))
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			downstream := httptest.NewServer(test.handler)
			defer downstream.Close()
			socket, stop := startTestBroker(t, &fakeAuthorizer{secret: []byte("response-canary")}, downstream.URL, uint32(os.Getuid()), uint32(os.Getgid()))
			defer stop()
			body, _ := json.Marshal(validServerRequest())
			request, _ := http.NewRequest(http.MethodPost, "http://unix/v1/broker/actions/github-get-repository", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response, err := unixClient(socket).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			result, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode == http.StatusOK || bytes.Contains(result, []byte("response-canary")) || bytes.Contains(result, []byte("secret downstream error")) {
				t.Fatalf("unsafe downstream failure status=%d body=%s", response.StatusCode, result)
			}
		})
	}
	if redirectCalls.Load() != 0 {
		t.Fatal("broker followed a downstream redirect")
	}
}

func FuzzBrokerRequestDecode(f *testing.F) {
	f.Add([]byte(`{"schema_version":1,"request_id":"42424242424242424242424242424242","deadline":"2026-07-17T00:00:00Z","capability":"x","owner":"x","repository":"x"}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 4096 {
			t.Skip()
		}
		decoder := json.NewDecoder(bytes.NewReader(input))
		decoder.DisallowUnknownFields()
		var request Request
		_ = decoder.Decode(&request)
	})
}
