package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
	"github.com/berryhill/aegis/internal/persistence/fleet"
	"github.com/berryhill/aegis/internal/reference"
)

func TestLoopAvailabilityHTTP(t *testing.T) {
	svc := apiService(t)
	configureAPIFleet(t, svc)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	svc.Config.API.Listen = address
	svc.Config.API.Console.Origin = "http://" + address
	ctx, cancel := context.WithCancel(context.Background())
	subject, err := svc.AuthenticateUnixPeer(ctx, uint32(os.Getuid()))
	if err != nil {
		t.Fatal(err)
	}
	owner := registerDoerWorkspaceAgent(t, svc, subject, "availability-owner", "owner-one").Revision
	foreign := registerDoerWorkspaceAgent(t, svc, subject, "availability-other", "owner-two").Revision
	text := "hello"
	rev, _, err := loop.NewDoerRevision("availability-loop", 1, "", loop.DoerContract{Task: "test availability", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishLoopAs(ctx, subject, app.PublishLoopInput{AgentID: owner.AgentID, Revision: rev, IdempotencyKey: "availability-publish"}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, svc) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	waitFor(t, "unix", svc.Config.API.UnixSocket)
	waitFor(t, "tcp", address)
	client := unixClient(svc.Config.API.UnixSocket)
	restartServer := func() {
		t.Helper()
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		client.CloseIdleConnections()
		ctx, cancel = context.WithCancel(context.Background())
		go func() { done <- Serve(ctx, svc) }()
		waitFor(t, "unix", svc.Config.API.UnixSocket)
		waitFor(t, "tcp", address)
	}
	path := "/v1/loops/" + rev.LoopID
	input := app.SetLoopAvailabilityInput{AgentID: owner.AgentID, Head: reference.RevisionRef{SchemaVersion: reference.RevisionRefSchemaVersion, ID: rev.LoopID, Revision: 1, Digest: rev.Digest}, EventID: "availability-archive", Sequence: 1, ExpectedState: loop.AvailabilityPublished, State: loop.AvailabilityArchived}
	// TCP has no Unix-peer principal: JSON identity must not authorize a mutation.
	// Probe before Console traffic consumes the same source-IP pre-auth bucket;
	// otherwise a timing-dependent 429 can mask the authentication boundary.
	req, _ := http.NewRequest(http.MethodPut, "http://"+address+path+"/availability", strings.NewReader(string(mustAvailabilityJSON(t, input))))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated mutation: %d", resp.StatusCode)
	}
	t.Run("malformed first change leaves availability history and audit unchanged", func(t *testing.T) {
		repository := svc.FleetRepository.(fleet.LoopAvailabilityRepository)
		before, err := svc.GetLoopViewAs(ctx, subject, rev.LoopID, 1)
		if err != nil {
			t.Fatal(err)
		}
		if before.Availability.Sequence != 0 || before.Availability.Digest != "" {
			t.Fatal("fixture is not the first availability change")
		}
		events, err := repository.ListLoopAvailabilityEvents(ctx, rev.LoopID)
		if err != nil {
			t.Fatal(err)
		}
		audits, err := svc.FleetRepository.AuditEvents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		unchanged := func(t *testing.T) {
			t.Helper()
			after, err := svc.GetLoopViewAs(ctx, subject, rev.LoopID, 1)
			if err != nil {
				t.Fatal(err)
			}
			nextEvents, err := repository.ListLoopAvailabilityEvents(ctx, rev.LoopID)
			if err != nil {
				t.Fatal(err)
			}
			nextAudits, err := svc.FleetRepository.AuditEvents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(events, nextEvents) || !reflect.DeepEqual(audits, nextAudits) {
				t.Fatal("rejected input mutated availability, exact history or audit")
			}
		}
		cases := []struct {
			name     string
			sequence uint64
			previous string
		}{
			{"sequence99_empty_predecessor", 99, ""},
			{"sequence1_nonempty_predecessor", 1, "sha256:" + strings.Repeat("b", 64)},
		}
		for _, tc := range cases {
			t.Run("API/"+tc.name, func(t *testing.T) {
				malformed := input
				malformed.Sequence, malformed.PreviousDigest = tc.sequence, tc.previous
				apiRequest(t, client, http.MethodPut, path+"/availability", malformed, nil, http.StatusBadRequest)
				unchanged(t)
			})
		}
		browser, csrf := loginConsole(t, address)
		for _, tc := range cases {
			t.Run("Console/"+tc.name, func(t *testing.T) {
				form := url.Values{"csrf": {csrf}, "revision": {"1"}, "digest": {rev.Digest}, "sequence": {strconv.FormatUint(tc.sequence, 10)}, "previous_digest": {tc.previous}, "expected_state": {"published"}, "state": {"archived"}}
				request, err := http.NewRequest(http.MethodPost, "http://"+address+"/console/loops/availability?loop_id="+rev.LoopID, strings.NewReader(form.Encode()))
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.Header.Set("Origin", "http://"+address)
				response, err := browser.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusBadRequest {
					body, _ := io.ReadAll(response.Body)
					t.Fatalf("Console POST got %d want 400: %s", response.StatusCode, body)
				}
				unchanged(t)
			})
		}
	})
	// Independent HTTP journeys retain durable state, not each other's source
	// and principal token buckets. Shut down fully before starting fresh middleware.
	restartServer()
	for _, raw := range []string{`{"unknown":true}`, `{} {}`} {
		req, _ := http.NewRequest(http.MethodPut, "http://unix"+path+"/availability", strings.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+svc.Config.API.Token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("strict decode: %d", resp.StatusCode)
		}
	}
	denied := input
	denied.AgentID = foreign.AgentID
	apiRequest(t, client, http.MethodPut, path+"/availability", denied, nil, http.StatusForbidden)
	stale := input
	stale.Head.Digest = "sha256:" + strings.Repeat("b", 64)
	apiRequest(t, client, http.MethodPut, path+"/availability", stale, nil, http.StatusConflict)
	var archived app.LoopAvailabilityResult
	apiRequest(t, client, http.MethodPut, path+"/availability", input, &archived, http.StatusCreated)
	var replay app.LoopAvailabilityResult
	apiRequest(t, client, http.MethodPut, path+"/availability", input, &replay, http.StatusOK)
	if !replay.Idempotent || replay.Event.Digest != archived.Event.Digest {
		t.Fatal("replay identity changed")
	}
	var list []app.LoopView
	apiRequest(t, client, http.MethodGet, "/v1/loops", nil, &list, http.StatusOK)
	if len(list) != 0 {
		t.Fatal("archived Loop discoverable")
	}
	apiRequest(t, client, http.MethodGet, "/v1/loops/inventory", nil, &list, http.StatusOK)
	if len(list) != 1 || list[0].Availability.State != loop.AvailabilityArchived {
		t.Fatalf("inventory: %+v", list)
	}
	var exact app.LoopView
	apiRequest(t, client, http.MethodGet, path+"/1", nil, &exact, http.StatusOK)
	if exact.Revision.Digest != rev.Digest {
		t.Fatal("exact revision changed")
	}
	var status loop.AvailabilityStatus
	apiRequest(t, client, http.MethodGet, path+"/availability", nil, &status, http.StatusOK)
	if status.State != loop.AvailabilityArchived {
		t.Fatal(status)
	}
	restore := input
	restore.EventID = "availability-restore"
	restore.Sequence = 2
	restore.ExpectedState = loop.AvailabilityArchived
	restore.State = loop.AvailabilityPublished
	restore.PreviousDigest = archived.Event.Digest
	badCAS := restore
	badCAS.Sequence = 3
	apiRequest(t, client, http.MethodPut, path+"/availability", badCAS, nil, http.StatusConflict)
	apiRequest(t, client, http.MethodPut, path+"/availability", restore, nil, http.StatusCreated)
	apiRequest(t, client, http.MethodGet, "/v1/loops", nil, &list, http.StatusOK)
	if len(list) != 1 {
		t.Fatal("restored Loop hidden")
	}
	// Exercise the actual authenticated browser route and rendered session CSRF.
	t.Run("rendered authenticated compose archive restore and exact history", func(t *testing.T) {
		restartServer()
		control, _, err := loop.NewDoerRevision("availability-control", 1, "", loop.DoerContract{Task: "published control", Workspace: t.TempDir(), WritableFiles: []string{"result.txt"}, VerifyFile: "result.txt", ExpectedText: &text, ExactBytes: true, MaxAttempts: 1})
		if err != nil {
			t.Fatal(err)
		}
		apiRequest(t, client, http.MethodPost, "/v1/loops", app.PublishLoopInput{AgentID: owner.AgentID, Revision: control, IdempotencyKey: "availability-control-publish"}, nil, http.StatusCreated)
		// A real lifecycle event makes history preservation non-vacuous. Retirement
		// and reversible discovery availability are independent dimensions.
		var retired app.LoopLifecycleResult
		apiRequest(t, client, http.MethodPut, path+"/lifecycle", app.SetLoopLifecycleInput{AgentID: owner.AgentID, Loop: input.Head, State: loop.LifecycleRetired, EventID: "availability-history-retired"}, &retired, http.StatusCreated)
		var before app.LoopView
		apiRequest(t, client, http.MethodGet, path+"/1", nil, &before, http.StatusOK)
		if len(before.History) != 1 || before.History[0].Digest != retired.Event.Digest {
			t.Fatal("fixture missing exact lifecycle history")
		}
		browser, csrf := loginConsole(t, address)
		compose := func(wantOption bool) {
			t.Helper()
			response, err := browser.Get("http://" + address + "/console/graphs/compose")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("authenticated compose: %d %v", response.StatusCode, err)
			}
			if !strings.Contains(string(body), `name="csrf" value="`+csrf+`"`) || !strings.Contains(string(body), `<option value="`+control.Digest+`">`) {
				t.Fatal("compose missing authenticated form or exact published control option")
			}
			if got := strings.Contains(string(body), `<option value="`+rev.Digest+`">`); got != wantOption {
				t.Fatalf("compose exact Loop option present=%v want=%v", got, wantOption)
			}
		}
		compose(true)
		getPage := func(query string, wants ...string) string {
			t.Helper()
			response, err := browser.Get("http://" + address + "/console/loops" + query)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != http.StatusOK {
				t.Fatalf("console GET: %d %v %s", response.StatusCode, err, body)
			}
			for _, want := range wants {
				if !strings.Contains(string(body), want) {
					t.Fatalf("missing rendered %q", want)
				}
			}
			return string(body)
		}
		publishedPage := getPage("", `name="availability"`)
		getPage("?record_key="+rev.LoopID+":1", `name="csrf" value="`+csrf+`"`, ">Archive</button>")
		post := func(values url.Values, origin string, authenticated bool, want int) {
			t.Helper()
			request, _ := http.NewRequest(http.MethodPost, "http://"+address+"/console/loops/availability?loop_id="+rev.LoopID, strings.NewReader(values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Origin", origin)
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			if authenticated {
				client.Jar = browser.Jar
			}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != want {
				body, _ := io.ReadAll(response.Body)
				t.Fatalf("console POST got %d want %d: %s", response.StatusCode, want, body)
			}
		}
		form := renderedLoopRowForm(t, publishedPage, rev.LoopID, rev.LoopID+":1", "Archive")
		apiRequest(t, client, http.MethodGet, path+"/availability", nil, &status, http.StatusOK)
		if form.Get("csrf") != csrf || form.Get("previous_digest") != status.Digest || form.Get("digest") != rev.Digest {
			t.Fatal("row form does not bind actual session and exact current state")
		}
		beforeAudits, err := svc.FleetRepository.AuditEvents(ctx)
		if err != nil {
			t.Fatal(err)
		}
		assertDeniedUnchanged := func() {
			t.Helper()
			after, err := svc.GetLoopViewAs(ctx, subject, rev.LoopID, 1)
			if err != nil {
				t.Fatal(err)
			}
			audits, err := svc.FleetRepository.AuditEvents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeAudits, audits) {
				t.Fatal("denied row mutation changed view or audit")
			}
		}
		origin := "http://" + address
		post(form, origin, false, http.StatusUnauthorized)
		assertDeniedUnchanged()
		post(form, "https://foreign.invalid", true, http.StatusForbidden)
		assertDeniedUnchanged()
		form.Set("csrf", "wrong")
		post(form, origin, true, http.StatusForbidden)
		assertDeniedUnchanged()
		form.Set("csrf", csrf)
		form.Set("principal_id", "forged")
		post(form, origin, true, http.StatusBadRequest)
		form.Del("principal_id")
		form.Set("agent_id", foreign.AgentID)
		post(form, origin, true, http.StatusBadRequest)
		form.Del("agent_id")
		form.Set("digest", "sha256:"+strings.Repeat("b", 64))
		post(form, origin, true, http.StatusConflict)
		form.Set("digest", rev.Digest)
		form.Set("previous_digest", "sha256:"+strings.Repeat("b", 64))
		post(form, origin, true, http.StatusConflict)
		form.Set("previous_digest", status.Digest)
		form.Set("sequence", "99")
		post(form, origin, true, http.StatusConflict)
		form.Set("sequence", "3")
		assertDeniedUnchanged()
		// The successful archive/restore journey gets its own server and real
		// login after the independent denial matrix. Never reset limiter internals.
		restartServer()
		browser, csrf = loginConsole(t, address)
		form = renderedLoopRowForm(t, getPage(""), rev.LoopID, rev.LoopID+":1", "Archive")
		post(form, origin, true, http.StatusSeeOther)
		post(form, origin, true, http.StatusConflict)
		compose(false)
		var historical app.LoopView
		apiRequest(t, client, http.MethodGet, path+"/1", nil, &historical, http.StatusOK)
		if historical.Availability.State != loop.AvailabilityArchived || !reflect.DeepEqual(historical.Revision, before.Revision) || !reflect.DeepEqual(historical.Validations, before.Validations) || !reflect.DeepEqual(historical.Provenance, before.Provenance) || !reflect.DeepEqual(historical.Lifecycle, before.Lifecycle) || !reflect.DeepEqual(historical.History, before.History) {
			t.Fatal("archive changed exact immutable revision, digest, validation, provenance or lifecycle history")
		}
		if body := getPage(""); strings.Contains(body, `id="record-`+rev.LoopID+`:1"`) {
			t.Fatal("archived Loop remains in published collection")
		}
		archivedPage := getPage("?availability=archived", ">Restore</button>", `name="csrf" value="`+csrf+`"`)
		getPage("?record_key="+rev.LoopID+":1", ">Restore</button>", rev.Digest, "Exact provenance", "Immutable lifecycle history", retired.Event.EventID, retired.Event.Digest)
		apiRequest(t, client, http.MethodGet, path+"/availability", nil, &status, http.StatusOK)
		form = renderedLoopRowForm(t, archivedPage, rev.LoopID, rev.LoopID+":1", "Restore")
		post(form, origin, true, http.StatusSeeOther)
		compose(true)
		getPage("?record_key="+rev.LoopID+":1", ">Archive</button>")
	})
}

func mustAvailabilityJSON(t *testing.T, value any) []byte {
	t.Helper()
	wire, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func TestLoopAvailabilityInputErrorBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"input", app.ErrLoopAvailabilityInput, http.StatusBadRequest},
		{"CAS", app.ErrConflict, http.StatusConflict},
		{"read failure", errors.New("availability read failed"), http.StatusInternalServerError},
		{"server event corruption", errors.New("invalid Loop availability event"), http.StatusInternalServerError},
		{"authoritative store corruption", fleet.ErrCorrupt, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _, _ := classifyError(tc.err)
			if status != tc.want {
				t.Fatalf("status=%d want=%d", status, tc.want)
			}
		})
	}
}
