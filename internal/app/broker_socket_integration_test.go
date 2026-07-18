//go:build linux && integration_sameuid

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/credentials/broker"
)

func TestPathnameSocketCredentialBrokerEndToEnd(t *testing.T) {
	s, token, canary, _ := brokerAuthorizedService(t)
	now := time.Now().UTC()
	s.Now = func() time.Time { return now }
	digest := sha256.Sum256([]byte(token))
	capability := s.capabilities[digest]
	capability.IssuedAt, capability.ExpiresAt = now, now.Add(time.Minute)
	s.capabilities[digest] = capability
	mandate, err := s.GetMandate(capability.MandateID)
	if err != nil {
		t.Fatal(err)
	}
	mandate.IssuedAt, mandate.ExpiresAt = now, now.Add(time.Minute)
	mandate.Subject.AuthenticatedAt, mandate.Subject.ExpiresAt = now, now.Add(time.Minute)
	if err = s.Store.Save("mandates", mandate.ID, mandate); err != nil {
		t.Fatal(err)
	}
	receivedAuthorization := make(chan string, 1)
	downstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		receivedAuthorization <- request.Header.Get("Authorization")
		response.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(response, `{"name":"approved-repository","owner":{"login":"approved-owner"},"private":true,"default_branch":"main","archived":false,"visibility":"private","updated_at":"2026-07-17T00:00:00Z","unapproved":"not returned"}`)
	}))
	defer downstream.Close()

	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(directory, "credential-broker.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := false
	go func() {
		done <- broker.ServeSameUIDForIntegrationTest(ctx, s, broker.ServerConfig{
			Socket:       socket,
			AllowedUID:   uint32(os.Getuid()),
			AllowedGID:   uint32(os.Getgid()),
			MaxBodyBytes: 4096,
			Timeout:      10 * time.Second,
			Destinations: map[string]string{broker.GitHubDestination: downstream.URL},
			Repositories: []string{"approved-owner/approved-repository"},
		})
	}()
	t.Cleanup(func() {
		if stopped {
			return
		}
		cancel()
		if err := <-done; err != nil {
			t.Errorf("broker shutdown: %v", err)
		}
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		if info, err := os.Lstat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pathname broker socket did not become ready")
		}
		time.Sleep(5 * time.Millisecond)
	}

	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
	requestBody, err := json.Marshal(validBrokerRequest(token, s.Now()))
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, "http://unix/v1/broker/actions/github-get-repository", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusOK || bytes.Contains(body, []byte(canary)) || bytes.Contains(body, []byte("unapproved")) {
		t.Fatalf("unsafe end-to-end result status=%d body=%s", response.StatusCode, body)
	}
	if got := <-receivedAuthorization; got != "Bearer "+canary {
		t.Fatalf("credential was not projected only to the downstream request: %q", got)
	}

	replay, _ := http.NewRequest(http.MethodPost, "http://unix/v1/broker/actions/github-get-repository", bytes.NewReader(requestBody))
	replay.Header.Set("Content-Type", "application/json")
	replayResponse, err := client.Do(replay)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, replayResponse.Body)
	replayResponse.Body.Close()
	if replayResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("replay status=%d", replayResponse.StatusCode)
	}

	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	stopped = true
	if _, err = os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remained after shutdown: %v", err)
	}
}
