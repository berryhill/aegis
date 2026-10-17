package command

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func upgradeConsoleHTML(authenticated bool) string {
	form := `<form method="post" action="/console/login"><input name="password" type="password"></form>`
	status := "Authentication required"
	if authenticated {
		form = `<form method="post" action="/console/logout"><input name="csrf" value="probe-csrf"></form>`
		status = "Authenticated control plane"
	}
	return `<html><head><title>Aegis Console</title><link rel="stylesheet" href="/console/assets/app.css"></head><body><header id="console-header">` + status + `</header><main id="main">` + form + `</main></body></html>`
}

func TestUpgradeAuthenticatedConsoleRejectsFallbackAndBrokenRendering(t *testing.T) {
	for _, mode := range []string{"working", "unrelated", "fallback", "broken-agents", "broken-state", "missing-asset", "oversized-login", "truncated-login"} {
		t.Run(mode, func(t *testing.T) {
			logins, logouts := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "unrelated" {
					fmt.Fprint(w, "ok")
					return
				}
				switch r.URL.Path {
				case "/console":
					if mode == "fallback" {
						http.Redirect(w, r, "/maintenance", http.StatusFound)
						return
					}
					fmt.Fprint(w, upgradeConsoleHTML(false))
				case "/console/login":
					logins++
					w.Header().Set("X-Aegis-Session-CSRF", "probe-csrf")
					http.SetCookie(w, &http.Cookie{Name: "aegis-console", Value: "probe-session", Path: "/console", HttpOnly: true})
					if mode == "oversized-login" || mode == "truncated-login" {
						w.Header().Set("Location", "/console/agents")
						if mode == "truncated-login" {
							w.Header().Set("Content-Length", "1000")
						}
						w.WriteHeader(http.StatusSeeOther)
						if mode == "oversized-login" {
							fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
						} else {
							fmt.Fprint(w, "short")
						}
						return
					}
					http.Redirect(w, r, "/console/agents", http.StatusSeeOther)
				case "/console/api/state":
					if mode == "broken-state" {
						http.Error(w, "unavailable", http.StatusServiceUnavailable)
						return
					}
					fmt.Fprint(w, `{"state":"ready","csrf":"probe-csrf"}`)
				case "/console/agents":
					if mode == "broken-agents" {
						fmt.Fprint(w, "<html>fallback error</html>")
						return
					}
					fmt.Fprint(w, upgradeConsoleHTML(true))
				case "/console/assets/app.css", "/console/assets/navigation.js":
					if mode == "missing-asset" {
						http.NotFound(w, r)
						return
					}
					fmt.Fprint(w, "fixture asset")
				case "/console/logout":
					if r.FormValue("csrf") != "probe-csrf" {
						http.Error(w, "invalid", http.StatusForbidden)
						return
					}
					logouts++
					http.Redirect(w, r, "/console", http.StatusSeeOther)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			err := verifyAuthenticatedUpgradeConsole(context.Background(), server.URL, []byte("fixture-password"))
			if (err == nil) != (mode == "working") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if (mode == "unrelated" || mode == "fallback") && logins != 0 {
				t.Fatal("unrecognized origin received protected input")
			}
			if mode != "unrelated" && mode != "fallback" && logouts != 1 {
				t.Fatal("probe session was not revoked")
			}
		})
	}
}
