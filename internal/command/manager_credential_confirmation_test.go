package command

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestProtectedValueTransportUncertaintyNeverReplays(t *testing.T) {
	for _, scenario := range []string{"dropped", "malformed", "trailing"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			c := &gatewayManagerClient{sessionID: "session", http: &http.Client{Transport: gatewayRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.GetBody != nil {
					t.Fatal("value is replayable")
				}
				_, _ = io.Copy(io.Discard, r.Body)
				if scenario == "dropped" {
					return nil, errors.New("synthetic transport failure")
				}
				body := "{"
				if scenario == "trailing" {
					body = `{"kind":"credential_created","origin":"aegis_authoritative","message":"ok"} {}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}}
			_, err := c.intakeValue(context.Background(), strings.Repeat("a", 48), []byte("synthetic-value"))
			if !errors.Is(err, errProtectedCreation) || calls != 1 {
				t.Fatalf("unsafe uncertainty: calls=%d err=%v", calls, err)
			}
		})
	}
}
