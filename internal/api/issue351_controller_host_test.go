package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
)

// Synthetic consent is confined to isolated fixtures and is not live approval.
func approveIssue351FixtureHost(t *testing.T, s *app.Service, d app.DoerDraft) {
	approveIssue351FixtureSetup(t, s, d, "host")
}

func approveIssue351FixtureProvision(t *testing.T, s *app.Service, d app.DoerDraft) {
	approveIssue351FixtureSetup(t, s, d, "provision")
}

func approveIssue351FixtureSetup(t *testing.T, s *app.Service, d app.DoerDraft, action string) {
	t.Helper()
	// Authenticate on the actual owning listener. A second console Manager's
	// password subject is correctly absent from Serve's live session registry.
	client := unixClient(s.Config.API.UnixSocket)
	var cookies []*http.Cookie
	csrf := ""
	origin := s.Config.API.Console.Origin
	call := func(path string, in any) *httptest.ResponseRecorder {
		raw, content := "", "application/json"
		if path == "login" {
			raw, content = in.(string), "application/octet-stream"
		} else {
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			raw = string(data)
		}
		req, err := http.NewRequest("POST", origin+"/console/loops/doer/setup-native/"+path, strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", content)
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", csrf)
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		rr := httptest.NewRecorder()
		rr.WriteHeader(response.StatusCode)
		if _, err := io.Copy(rr.Body, response.Body); err != nil {
			t.Fatal(err)
		}
		if path == "login" {
			cookies = response.Cookies()
		}
		return rr
	}
	login := call("login", "api-principal-password")
	var authenticated struct {
		CSRF string `json:"csrf"`
	}
	if login.Code != http.StatusOK || json.Unmarshal(login.Body.Bytes(), &authenticated) != nil || authenticated.CSRF == "" || len(cookies) != 1 {
		t.Fatalf("isolated owning-listener login: %d", login.Code)
	}
	csrf = authenticated.CSRF
	defer func() {
		if rr := call("logout", app.DoerProtectedSetupInput{}); rr.Code != http.StatusOK {
			t.Errorf("isolated native logout: %d", rr.Code)
		}
	}()
	rr := call("review", app.DoerProtectedSetupInput{ID: d.ID, ExpectedVersion: d.Version, Action: action})
	var review app.DoerProtectedSetupReview
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &review) != nil || review.Agent == nil || review.Charter == nil || review.Receipt == "" || review.Agent.Ownership.OwnerID == d.PrincipalID {
		t.Fatalf("exact protected participant %s review: status=%d", action, rr.Code)
	}
	if action == "host" && (review.Host == nil || review.Host.ExpectedCandidateDigest != d.HostRun.Loop.Digest) {
		t.Fatal("host review substituted exact candidate")
	}
	if action == "provision" && (review.Plan == nil || review.Plan.CharterDigest != review.Charter.Digest || review.Plan.AgentID != d.Agent.ID) {
		t.Fatal("provision review substituted exact charter")
	}
	// Requesting review did not grant host scope.
	sub, err := s.AuthenticateUnixPeer(context.Background(), uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.ReadDoerDraftAs(context.Background(), sub, d.ID)
	if err != nil || !reflect.DeepEqual(current, review.Draft) {
		t.Fatalf("protected review roundtrip drift: read=%v host_run_equal=%t contract_equal=%t", err, reflect.DeepEqual(current.HostRun, review.Draft.HostRun), reflect.DeepEqual(current.Contract, review.Draft.Contract))
	}
	if _, err := s.ReadDoerHostWriteApprovalAs(context.Background(), sub, d.Agent, d.Contract); err == nil {
		t.Fatal("review minted approval")
	}
	rr = call("decision", app.DoerProtectedSetupDecision{Receipt: review.Receipt, Decision: "approve"})
	var result app.DoerProtectedSetupResult
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &result) != nil {
		t.Fatalf("exact protected participant %s decision: status=%d", action, rr.Code)
	}
	if action == "host" {
		if result.Host == nil || result.Provisioning != nil || result.Host.ControllerSubjectID == "" || result.Host.OwnerID != review.Agent.Ownership.OwnerID || result.Host.PrincipalID != d.PrincipalID || result.Host.Candidate != d.HostRun.Loop || result.Host.DoerReusable == nil {
			t.Fatal("host decision changed scope or provisioned")
		}
	} else if action == "provision" {
		r := result.Provisioning
		if r == nil || result.Host != nil || r.Status != "verified" || r.PlanID != review.Plan.ID || r.PlanDigest != review.Plan.Digest || r.CharterDigest != review.Charter.Digest {
			t.Fatal("provision decision changed exact plan/charter or granted host scope")
		}
		approval, err := s.GetApproval(r.ApprovalID)
		if err != nil || approval.Status != "consumed" {
			t.Fatal("fixture provision approval not consumed", err)
		}
		if _, err := s.ReadDoerHostWriteApprovalAs(context.Background(), sub, d.Agent, d.Contract); err == nil {
			t.Fatal("provision decision granted host scope")
		}
	} else {
		t.Fatal("unknown fixture setup action")
	}
	if _, err := os.Stat(d.Contract.Workspace + "/" + d.Contract.VerifyFile); !os.IsNotExist(err) {
		t.Fatal("independent setup executed task output", err)
	}
	if rr = call("decision", app.DoerProtectedSetupDecision{Receipt: review.Receipt, Decision: "approve"}); rr.Code < 400 {
		t.Fatal("review receipt replayed")
	}
}
