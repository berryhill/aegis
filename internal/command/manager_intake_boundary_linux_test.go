//go:build linux

package command

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/managergateway"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

type failingIntakeReview struct{}

func (failingIntakeReview) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

func TestProtectedReviewOutputFailureStopsBeforeApproval(t *testing.T) {
	master, slave := openCommandPTY(t)
	defer master.Close()
	defer slave.Close()
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(slave)
	cmd.SetOut(slave)
	calls := 0
	client := &gatewayManagerClient{sessionID: "session", protectedIntake: true, http: &http.Client{Transport: gatewayRoundTripper(func(r *http.Request) (*http.Response, error) {
		var input managergateway.CredentialIntakeRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error("invalid cleanup request")
		}
		if input.Action != "cancel" {
			t.Error("failed review allowed an operation other than cancel")
		}
		calls++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"kind":"credential_creation_cancelled","origin":"aegis_authoritative","message":"cancelled"}`))}, nil
	})}}
	h := &managergateway.CredentialIntakeHandoff{Protocol: managergateway.ProtectedIntakeProtocol, OperationID: strings.Repeat("a", 48), SessionID: "session", PrincipalID: "principal", ExpiresAt: time.Now().Add(time.Minute), Stage: "metadata"}
	result := managergateway.TurnResult{Kind: "credential_protected_intake", Origin: managergateway.TurnOriginAuthoritative, Message: "review", Intake: h}
	if err := runGatewayCredentialIntake(context.Background(), cmd, client, result, failingIntakeReview{}); err == nil {
		t.Fatal("output failure resumed conversation")
	}
	if calls != 1 {
		t.Fatal("operation cleanup missing")
	}
	assertCommandPTYRestored(t, slave, initial)
	cmd.SetOut(io.Discard)
	if gatewayProtectedIntakeAvailable(cmd) {
		t.Fatal("redirected review output advertised protected intake")
	}
}

func TestProtectedFlushReportsFailure(t *testing.T) {
	master, slave := openCommandPTY(t)
	defer master.Close()
	if err := slave.Close(); err != nil {
		t.Fatal(err)
	}
	if err := discardProtectedTerminalInput(slave); err == nil {
		t.Fatal("failed fresh-input flush was ignored")
	}
}
