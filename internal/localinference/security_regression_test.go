//go:build linux

package localinference

import (
	"context"
	"errors"
	"github.com/berryhill/aegis/internal/core"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testRoute(url string) core.LocalInference {
	return core.LocalInference{Kind: "ollama", Endpoint: url, ModelDigest: "sha256:" + strings.Repeat("a", 64)}
}
func TestInventoryStrictMetadata(t *testing.T) {
	good := `{"name":"exact","model":"exact","digest":"` + strings.Repeat("a", 64) + `","details":{"format":"gguf"}}`
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"metadata", `{"models":[` + good + `]}`, true},
		{"duplicate", `{"models":[],"models":[` + good + `]}`, false},
		{"nested duplicate", `{"models":[` + good + `],"metadata":{"x":1,"x":2}}`, false},
		{"depth", `{"models":[` + good + `],"metadata":` + strings.Repeat("[", 34) + `0` + strings.Repeat("]", 34) + `}`, false},
		{"alias", `{"models":[` + strings.Replace(good, `"model":"exact"`, `"model":"other"`, 1) + `]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, tc.body) }))
			defer s.Close()
			p, e := New(testRoute(s.URL), "exact", func(context.Context) error { return nil })
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			if e = p.VerifyModel(context.Background()); (e == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, e)
			}
		})
	}
}

func TestBlockedGenerationLifetimeCancellation(t *testing.T) {
	for _, mode := range []string{"revocation", "expiry", "caller", "close"} {
		t.Run(mode, func(t *testing.T) {
			entered, stopped := make(chan struct{}), make(chan struct{})
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/tags" {
					io.WriteString(w, `{"models":[{"name":"exact","digest":"`+strings.Repeat("a", 64)+`"}]}`)
					return
				}
				io.Copy(io.Discard, r.Body)
				close(entered)
				<-r.Context().Done()
				close(stopped)
			}))
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "expiry" {
				ctx, cancel = context.WithTimeout(ctx, 400*time.Millisecond)
				defer cancel()
			}
			var revoked atomic.Bool
			p, e := NewWithContext(ctx, testRoute(s.URL), "exact", func(context.Context) error {
				if revoked.Load() {
					return errors.New("revoked")
				}
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			if e = p.Bind(os.Getpid()); e != nil {
				t.Fatal(e)
			}
			req, _ := http.NewRequest("POST", p.Endpoint()+"/v1/chat/completions", strings.NewReader(`{"model":"exact","messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Authorization", "Bearer "+CompatibilityToken)
			done := make(chan struct{})
			go func() {
				defer close(done)
				r, e := http.DefaultClient.Do(req)
				if e == nil {
					r.Body.Close()
				}
			}()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("generation not reached")
			}
			switch mode {
			case "revocation":
				revoked.Store(true)
			case "caller":
				cancel()
			case "close":
				p.Close()
			}
			select {
			case <-stopped:
			case <-time.After(2 * time.Second):
				t.Fatal("upstream context not cancelled")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("request leaked")
			}
		})
	}
}

func TestCustodyExactAddressesAndExit(t *testing.T) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	client, e := net.Dial("tcp4", l.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer client.Close()
	accepted, e := l.Accept()
	if e != nil {
		t.Fatal(e)
	}
	defer accepted.Close()
	c, e := AcquireProcessCustody(os.Getpid())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if !c.OwnsTCPConnection(accepted.RemoteAddr(), accepted.LocalAddr()) {
		t.Fatal("exact tuple denied")
	}
	wrong := *accepted.RemoteAddr().(*net.TCPAddr)
	wrong.IP = net.ParseIP("127.0.0.2")
	if c.OwnsTCPConnection(&wrong, accepted.LocalAddr()) {
		t.Fatal("wrong source address accepted")
	}
	wrong = *accepted.LocalAddr().(*net.TCPAddr)
	wrong.IP = net.ParseIP("127.0.0.2")
	if c.OwnsTCPConnection(accepted.RemoteAddr(), &wrong) {
		t.Fatal("wrong destination accepted")
	}
	cmd := exec.Command("/bin/sh", "-c", "read x")
	stdin, _ := cmd.StdinPipe()
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	child, e := AcquireProcessCustody(cmd.Process.Pid)
	if e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal(e)
	}
	defer child.Close()
	stdin.Close()
	cmd.Wait()
	if child.Alive() || child.OwnsTCPConnection(accepted.RemoteAddr(), accepted.LocalAddr()) {
		t.Fatal("exited custody accepted")
	}
}

func TestCompatibilityHeaderIsExact(t *testing.T) {
	p, e := New(testRoute("http://127.0.0.1:1"), "exact", func(context.Context) error { t.Error("invalid header admitted"); return nil })
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	for _, header := range []string{"", "Bearer wrong", "bearer " + CompatibilityToken} {
		r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		p.serve(w, r)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
}
