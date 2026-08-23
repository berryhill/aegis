package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/credentials"
	credentialbbolt "github.com/berryhill/aegis/internal/credentials/bbolt"
	consoleweb "github.com/berryhill/aegis/web/console"
)

func TestCredentialOperationFormDecoderAcceptsExactCreateAndKeepsValueOutOfPresentation(t *testing.T) {
	value := make([]byte, 48)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	values := url.Values{
		"csrf":      {"csrf-proof"},
		"operation": {"create"},
		"reference": {"provider/primary"},
		"kind":      {"api_token"},
		"value":     {string(value)},
	}
	request := httptest.NewRequest(http.MethodPost, "/console/credentials/operation/review", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	form, err := decodeCredentialOperationForm(request)
	if err != nil {
		t.Fatal(err)
	}
	defer wipeBytes(form.Value)
	if form.Operation != "create" || form.Reference != "provider/primary" || form.Kind != "api_token" || !bytes.Equal(form.Value, value) {
		t.Fatal("decoded credential form did not preserve the exact non-secret fields and protected value")
	}
	model := credentialOperationModel(form, "review")
	if model.Operation != form.Operation || model.Reference != form.Reference || model.Kind != form.Kind {
		t.Fatalf("credential review model lost metadata: %+v", model)
	}
	if bytes.Contains([]byte(model.Status+model.ReasonCode+model.Destinations), value) {
		t.Fatal("credential review presentation exposed the protected value")
	}
}

func TestCredentialOperationFormDecoderFailsClosed(t *testing.T) {
	valid := url.Values{
		"csrf":      {"csrf-proof"},
		"operation": {"revoke"},
		"record_id": {"credential-1"},
		"version":   {"2"},
		"reason":    {"operator-request"},
	}
	for name, mutate := range map[string]func(url.Values){
		"missing csrf":          func(values url.Values) { values.Del("csrf") },
		"unknown authority":     func(values url.Values) { values.Set("principal_id", "forged") },
		"duplicate operation":   func(values url.Values) { values["operation"] = []string{"revoke", "backup"} },
		"unsupported operation": func(values url.Values) { values.Set("operation", "export") },
		"zero version":          func(values url.Values) { values.Set("version", "0") },
		"malformed version":     func(values url.Values) { values.Set("version", "latest") },
		"missing reason":        func(values url.Values) { values.Del("reason") },
	} {
		t.Run(name, func(t *testing.T) {
			values := cloneURLValues(valid)
			mutate(values)
			request := httptest.NewRequest(http.MethodPost, "/console/credentials/operation/review", strings.NewReader(values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if form, err := decodeCredentialOperationForm(request); err == nil {
				wipeBytes(form.Value)
				t.Fatal("unsafe credential operation form accepted")
			}
		})
	}

	oversized := httptest.NewRequest(http.MethodPost, "/console/credentials/operation/review", strings.NewReader(strings.Repeat("x", maxCredentialFormBytes+1)))
	oversized.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if _, err := decodeCredentialOperationForm(oversized); err == nil {
		t.Fatal("oversized credential operation form accepted")
	}
}

func TestCredentialReviewPayloadRoundTripIsExactAndStrict(t *testing.T) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	form := credentialOperationForm{Operation: "rotate", RecordID: "credential-1", Value: append([]byte(nil), value...)}
	defer wipeBytes(form.Value)
	payload, err := encodeCredentialReview(form)
	if err != nil {
		t.Fatal(err)
	}
	defer wipeBytes(payload)
	decoded, err := decodeCredentialReview(payload)
	if err != nil {
		t.Fatal(err)
	}
	defer wipeBytes(decoded.Value)
	if decoded.Operation != form.Operation || decoded.RecordID != form.RecordID || !bytes.Equal(decoded.Value, value) {
		t.Fatal("credential review payload did not round-trip exactly")
	}
	if _, err = decodeCredentialReview(append([]byte(`{"authority":"forged",`), payload[1:]...)); err == nil {
		t.Fatal("credential review payload accepted an unknown authority field")
	}
	if _, err = decodeCredentialReview(append(payload, payload...)); err == nil {
		t.Fatal("credential review payload accepted trailing content")
	}
}

func TestCredentialPaginationIsStableAndPreservesCollectionControls(t *testing.T) {
	model := consoleweb.SurfaceModel{Records: []consoleweb.RecordModel{
		{Key: "credential-1"}, {Key: "credential-2"}, {Key: "credential-3"}, {Key: "credential-4"}, {Key: "credential-5"},
	}}
	query := url.Values{"q": {"provider"}, "status": {"active"}, "limit": {"2"}, "page": {"2"}, "record_key": {"credential-3"}}
	if err := paginateConsoleCredentials(&model, "2", 2, query); err != nil {
		t.Fatal(err)
	}
	if len(model.Records) != 2 || model.Records[0].Key != "credential-3" || model.Records[1].Key != "credential-4" {
		t.Fatalf("credential page was not stable: %+v", model.Records)
	}
	if model.Pagination.Label != "Page 2 of 3" || model.Pagination.Summary != "Showing 3–4 of 5 matching records" {
		t.Fatalf("credential pagination counts are not authoritative: %+v", model.Pagination)
	}
	if model.Pagination.PreviousURL != "/console/credentials?limit=2&q=provider&status=active#/credentials" || model.Pagination.NextURL != "/console/credentials?limit=2&page=3&q=provider&status=active#/credentials" {
		t.Fatalf("credential pagination did not preserve controls or remove deep-link resolution: %+v", model.Pagination)
	}
	for _, page := range []string{"0", "-1", "4", "10001", "not-a-page"} {
		candidate := consoleweb.SurfaceModel{Records: make([]consoleweb.RecordModel, 5)}
		if err := paginateConsoleCredentials(&candidate, page, 2, nil); err == nil {
			t.Fatalf("invalid credential page %q accepted", page)
		}
	}
}

func cloneURLValues(values url.Values) url.Values {
	clone := url.Values{}
	for key, entries := range values {
		clone[key] = append([]string(nil), entries...)
	}
	return clone
}

type credentialRouteFixture struct {
	t       *testing.T
	svc     *app.Service
	ctx     context.Context
	address string
	client  *http.Client
	csrf    string
}

func newCredentialRouteFixture(t *testing.T) *credentialRouteFixture {
	t.Helper()
	return newCredentialRouteFixtureWithTTL(t, 2*time.Minute)
}

func newCredentialRouteFixtureWithTTL(t *testing.T, sessionTTL time.Duration) *credentialRouteFixture {
	t.Helper()
	svc := apiService(t)
	configureAPIFleet(t, svc)
	root := filepath.Join(t.TempDir(), "credentials")
	keyPath, databasePath := filepath.Join(root, "authority.kek"), filepath.Join(root, "authority.db")
	if err := credentials.CreateHostKey(keyPath, "console-test-kek"); err != nil {
		t.Fatal(err)
	}
	custodian, err := credentials.LoadFileCustodian(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	credentialStore, err := credentialbbolt.Open(context.Background(), databasePath, "deployment-test", custodian)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = credentialStore.Close() })
	svc.Config.Credentials.Authority = config.CredentialAuthority{Database: databasePath, DeploymentID: "deployment-test", Custody: "host-file", KEKFile: keyPath}
	svc.ConfigureCredentials(credentialStore, custodian)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	_ = probe.Close()
	svc.Config.API.Listen, svc.Config.API.Console.Origin = address, "http://"+address
	svc.Config.API.Console.SessionTTL = sessionTTL
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	waitFor(t, "tcp", address)
	fixture := &credentialRouteFixture{t: t, svc: svc, ctx: ctx, address: address}
	fixture.client, fixture.csrf = fixture.newSession()
	return fixture
}

func (f *credentialRouteFixture) newSession() (*http.Client, string) {
	f.t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	login, _ := http.NewRequest(http.MethodPost, "http://"+f.address+"/console/login", strings.NewReader(url.Values{"password": {"api-principal-password"}}.Encode()))
	login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	login.Header.Set("Origin", "http://"+f.address)
	response, err := client.Do(login)
	if err != nil {
		f.t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		f.t.Fatalf("principal-password login status=%d", response.StatusCode)
	}
	stateResponse, err := client.Get("http://" + f.address + "/console/api/state")
	if err != nil {
		f.t.Fatal(err)
	}
	var state struct {
		CSRF string `json:"csrf"`
	}
	err = json.NewDecoder(stateResponse.Body).Decode(&state)
	_ = stateResponse.Body.Close()
	if err != nil || stateResponse.StatusCode != http.StatusOK || state.CSRF == "" {
		f.t.Fatalf("session state status=%d csrf=%t err=%v", stateResponse.StatusCode, state.CSRF != "", err)
	}
	return client, state.CSRF
}

func (f *credentialRouteFixture) post(client *http.Client, path string, values url.Values) (*http.Response, []byte) {
	f.t.Helper()
	// Keep the integration workflow beneath the server's intentional five-request-per-second source limit.
	time.Sleep(210 * time.Millisecond)
	request, err := http.NewRequest(http.MethodPost, "http://"+f.address+path, strings.NewReader(values.Encode()))
	if err != nil {
		f.t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "http://"+f.address)
	response, err := client.Do(request)
	if err != nil {
		f.t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	return response, body
}

func (f *credentialRouteFixture) review(values url.Values) string {
	f.t.Helper()
	values.Set("csrf", f.csrf)
	response, body := f.post(f.client, "/console/credentials/operation/review", values)
	secret := []byte(values.Get("value"))
	if response.StatusCode != http.StatusOK || (len(secret) > 0 && bytes.Contains(body, secret)) || !bytes.Contains(body, []byte("Review prepared")) {
		f.t.Fatalf("credential review status=%d body=%s", response.StatusCode, body)
	}
	marker := []byte(`name="receipt" value="`)
	start := bytes.Index(body, marker)
	if start < 0 {
		f.t.Fatalf("credential review omitted receipt: %s", body)
	}
	start += len(marker)
	end := bytes.IndexByte(body[start:], '"')
	if end != 64 {
		f.t.Fatalf("credential review emitted malformed receipt length=%d", end)
	}
	return string(body[start : start+end])
}

func (f *credentialRouteFixture) execute(receipt string, want int) []byte {
	f.t.Helper()
	response, body := f.post(f.client, "/console/credentials/operation/execute", url.Values{"csrf": {f.csrf}, "receipt": {receipt}})
	if response.StatusCode != want {
		f.t.Fatalf("credential execute status=%d want=%d body=%s", response.StatusCode, want, body)
	}
	return body
}

func (f *credentialRouteFixture) principal() core.Subject {
	return core.Subject{ID: "route-test", PrincipalID: f.svc.Config.Principal.ID, ExpiresAt: time.Now().Add(time.Minute)}
}

func TestConsoleCredentialRoutesCoverAllOperationsAndFailClosedReceipts(t *testing.T) {
	f := newCredentialRouteFixture(t)
	createSecret := "create-route-canary-not-for-rendering"
	createReceipt := f.review(url.Values{"operation": {"create"}, "reference": {"provider-primary"}, "kind": {"api-token"}, "value": {createSecret}})
	page, err := f.svc.QueryCredentialsAs(f.ctx, f.principal(), app.CredentialCollectionQuery{Search: "provider-primary", Page: 1, Limit: 10})
	if err != nil || page.Total != 0 {
		t.Fatalf("review mutated credential authority: page=%+v err=%v", page, err)
	}
	body := f.execute(createReceipt, http.StatusOK)
	if bytes.Contains(body, []byte(createSecret)) || !bytes.Contains(body, []byte("metadata-only authoritative readback")) {
		t.Fatalf("create response was not metadata-only: %s", body)
	}
	f.execute(createReceipt, http.StatusForbidden)
	page, err = f.svc.QueryCredentialsAs(f.ctx, f.principal(), app.CredentialCollectionQuery{Search: "provider-primary", Page: 1, Limit: 10})
	if err != nil || page.Total != 1 || len(page.Records) != 1 {
		t.Fatalf("created credential missing: page=%+v err=%v", page, err)
	}
	recordID := page.Records[0].ID

	rotateSecret := "rotate-route-canary-not-for-rendering"
	body = f.execute(f.review(url.Values{"operation": {"rotate"}, "record_id": {recordID}, "value": {rotateSecret}}), http.StatusOK)
	if bytes.Contains(body, []byte(rotateSecret)) {
		t.Fatalf("rotate response exposed protected value: %s", body)
	}
	record, err := f.svc.CredentialAs(f.ctx, f.principal(), recordID)
	if err != nil || record.CurrentVersion != 2 {
		t.Fatalf("rotation readback=%+v err=%v", record, err)
	}
	f.execute(f.review(url.Values{"operation": {"bind"}, "record_id": {recordID}, "agent_id": {"agent-1"}, "stanza_id": {"principal"}, "deployment_id": {"deployment-test"}, "scope": {"provider-test"}, "destinations": {"api.example.test"}, "mode": {"brokered"}, "version_policy": {"current"}}), http.StatusOK)
	f.execute(f.review(url.Values{"operation": {"backup"}}), http.StatusOK)
	if info, statErr := os.Stat(f.svc.Config.Credentials.Authority.Database + ".backup"); statErr != nil || info.Size() == 0 {
		t.Fatalf("policy-selected backup missing: info=%v err=%v", info, statErr)
	}

	cancelReceipt := f.review(url.Values{"operation": {"create"}, "reference": {"cancelled-provider"}, "kind": {"api-token"}, "value": {"cancel-route-canary"}})
	response, cancelBody := f.post(f.client, "/console/credentials/operation/cancel", url.Values{"csrf": {f.csrf}, "receipt": {cancelReceipt}})
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/console/credentials#/credentials" {
		t.Fatalf("cancel status=%d location=%q body=%s", response.StatusCode, response.Header.Get("Location"), cancelBody)
	}
	f.execute(cancelReceipt, http.StatusForbidden)
	cancelled, err := f.svc.QueryCredentialsAs(f.ctx, f.principal(), app.CredentialCollectionQuery{Search: "cancelled-provider", Page: 1, Limit: 10})
	if err != nil || cancelled.Total != 0 {
		t.Fatalf("cancelled operation mutated authority: page=%+v err=%v", cancelled, err)
	}

	crossReceipt := f.review(url.Values{"operation": {"backup"}})
	otherClient, otherCSRF := f.newSession()
	response, _ = f.post(otherClient, "/console/credentials/operation/execute", url.Values{"csrf": {otherCSRF}, "receipt": {crossReceipt}})
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-session receipt status=%d", response.StatusCode)
	}
	response, _ = f.post(f.client, "/console/credentials/operation/cancel", url.Values{"csrf": {f.csrf}, "receipt": {crossReceipt}})
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("owner cancellation after cross-session denial status=%d", response.StatusCode)
	}
	response, _ = f.post(f.client, "/console/credentials/operation/review", url.Values{"operation": {"backup"}})
	if response.StatusCode != http.StatusBadRequest && response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing-CSRF review status=%d", response.StatusCode)
	}

	staleReceipt := f.review(url.Values{"operation": {"revoke"}, "record_id": {recordID}, "version": {"2"}, "reason": {"stale-review"}})
	if _, err = f.svc.RotateCredentialAs(f.ctx, f.principal(), recordID, app.RotateCredentialInput{Value: []byte("server-side-drift")}); err != nil {
		t.Fatal(err)
	}
	f.execute(staleReceipt, http.StatusConflict)
	f.execute(f.review(url.Values{"operation": {"revoke"}, "record_id": {recordID}, "version": {"3"}, "reason": {"operator-request"}}), http.StatusOK)
	useErr := f.svc.CredentialAuthority.Use(f.ctx, credentials.CredentialBindingKey{AgentID: "agent-1", StanzaID: "principal", DeploymentID: "deployment-test", Scope: "provider-test"}, "api.example.test", func([]byte) error { return nil })
	if !errors.Is(useErr, credentials.ErrRevoked) {
		t.Fatalf("revoked exact current version remained usable: %v", useErr)
	}
}

func TestConsoleCredentialReviewDeniesExpiredSession(t *testing.T) {
	// Leave enough time for the deliberately expensive principal-password
	// verification and CSRF readback under the race detector. post() adds a
	// further rate-limit delay after the session has expired.
	f := newCredentialRouteFixtureWithTTL(t, 3*time.Second)
	time.Sleep(3100 * time.Millisecond)
	response, body := f.post(f.client, "/console/credentials/operation/review", url.Values{"csrf": {f.csrf}, "operation": {"backup"}})
	if response.StatusCode != http.StatusUnauthorized || !bytes.Contains(body, []byte(`"code":"unauthenticated"`)) {
		t.Fatalf("expired credential session status=%d body=%s", response.StatusCode, body)
	}
}
