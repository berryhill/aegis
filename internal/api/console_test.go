package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/berryhill/aegis/internal/app"
)

func TestConsoleFormDecoderAcceptsOneExactBoundedField(t *testing.T) {
	valid := httptest.NewRequest("POST", "/console/session", strings.NewReader("bootstrap=single-use%2Btoken"))
	valid.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	value, err := decodeConsoleForm(valid, "bootstrap")
	if err != nil || value != "single-use+token" {
		t.Fatalf("valid native form value=%q err=%v", value, err)
	}

	for name, request := range map[string]*http.Request{
		"wrong content type": httptest.NewRequest("POST", "/console/session", strings.NewReader("bootstrap=value")),
		"unknown field":      httptest.NewRequest("POST", "/console/session", strings.NewReader("bootstrap=value&authority=admin")),
		"duplicate field":    httptest.NewRequest("POST", "/console/session", strings.NewReader("bootstrap=one&bootstrap=two")),
		"oversized":          httptest.NewRequest("POST", "/console/session", bytes.NewReader(bytes.Repeat([]byte("x"), 8193))),
	} {
		t.Run(name, func(t *testing.T) {
			if name != "wrong content type" {
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			if _, err := decodeConsoleForm(request, "bootstrap"); err == nil {
				t.Fatal("unsafe native form accepted")
			}
		})
	}
}

func TestConsoleSignalsAreStrictAndPresentationOnly(t *testing.T) {
	for name, raw := range map[string]string{
		"authority": `{"authority":"admin"}`,
		"trailing":  `{"csrf":"ok"}{}`,
		"oversized": `{"csrf":"` + strings.Repeat("x", 9000) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/console/fragments/surface?datastar="+url.QueryEscape(raw), nil)
			if err := validateConsoleSignals(request); err == nil {
				t.Fatal("unsafe signals accepted")
			}
		})
	}
	request := httptest.NewRequest("GET", "/console/fragments/surface?datastar="+url.QueryEscape(`{"csrf":"presentation-only"}`), nil)
	if err := validateConsoleSignals(request); err != nil {
		t.Fatalf("closed presentation signals denied: %v", err)
	}
}

func TestConsoleDomainAndRecordSelectorsFailClosed(t *testing.T) {
	for _, raw := range []string{"authority", "../agents", "agents&stanza=admin"} {
		if _, err := parseConsoleDomain(raw); err == nil {
			t.Fatalf("forged domain %q accepted", raw)
		}
	}
	model, err := consoleSurfaceModel(app.FleetSurface{Readiness: map[string]app.SurfaceReadiness{"registry": {State: "empty", Authoritative: true}}}, consoleAgents)
	if err != nil {
		t.Fatal(err)
	}
	if model.State != "empty" {
		t.Fatalf("authoritative empty state=%q", model.State)
	}
	for _, raw := range []string{"", "-1", "0", "stanza-admin"} {
		if err = selectConsoleRecord(&model, raw); err == nil {
			t.Fatalf("forged record selector %q accepted", raw)
		}
	}
}

func TestConsoleRenderIsBoundedAndCancellationAware(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	minimal := templ.ComponentFunc(func(_ context.Context, writer io.Writer) error { _, err := io.WriteString(writer, "safe"); return err })
	if _, err := renderConsole(cancelled, minimal); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled render error=%v", err)
	}
	oversized := templ.ComponentFunc(func(_ context.Context, writer io.Writer) error {
		_, err := writer.Write([]byte(strings.Repeat("x", maxConsolePatchBytes+1)))
		return err
	})
	if _, err := renderConsole(context.Background(), oversized); err == nil || !strings.Contains(err.Error(), "bounded") {
		t.Fatalf("oversized render error=%v", err)
	}
}

func TestConsolePatchUsesOneRequestScopedDatastarEvent(t *testing.T) {
	request := httptest.NewRequest("GET", "http://console.test/console/fragments/surface", nil)
	recorder := httptest.NewRecorder()
	component := templ.ComponentFunc(func(_ context.Context, writer io.Writer) error {
		_, err := io.WriteString(writer, `<main id="workspace">escaped</main>`)
		return err
	})
	if err := patchConsole(recorder, request, component); err != nil {
		t.Fatal(err)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "text/event-stream" {
		t.Fatalf("content type=%q", contentType)
	}
	body := recorder.Body.String()
	if strings.Count(body, "event: datastar-patch-elements") != 1 || !strings.Contains(body, "data: elements <main id=\"workspace\">escaped</main>") {
		t.Fatalf("unexpected bounded SSE framing: %q", body)
	}
}
