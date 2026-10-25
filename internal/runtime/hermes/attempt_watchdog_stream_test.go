package hermes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
)

// Models a parsed event followed by silence. A separate local control socket
// releases completion, while the derived native idle timeout remains enforced.
// No provider client, native credential loader or API is involved.
const controlledWatchdog = `
  if request['params']['text']=='controlled':
   import socket
   control=socket.create_connection(CONTROL_ADDRESS)
   control.sendall((json.dumps({'pid':os.getpid(),'timeout':value,'toolsets':os.environ.get('HERMES_TUI_TOOLSETS'),'auth':os.path.isfile('auth.json')})+'\n').encode())
   ready,_,_=select.select([sys.stdin,control],[],[],float(value) if value else 12.0)
   if control in ready and control.recv(1)==b'!':
    event('message.complete',{'status':'complete','text':value})
   else:
    event('message.complete',{'status':'error','error_surface':{'layer':'streaming','code':'timeout'}})
   control.close()
   continue
`

func TestCodexWatchdogControlledStream(t *testing.T) {
	for _, kind := range []string{"missing-native-knob", "quiet-beyond-native-default", "inflight-cancel", "turn-expiry", "authority-expiry", "token-expiry"} {
		t.Run(kind, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			address := listener.Addr().(*net.TCPAddr)
			control := strings.Replace(controlledWatchdog, "CONTROL_ADDRESS", fmt.Sprintf("('127.0.0.1',%d)", address.Port), 1)
			script := strings.Replace(watchdogGateway, "  if not value or float(value)<=0:", control+"  if not value or float(value)<=0:", 1)
			if kind == "missing-native-knob" {
				// Negative control: omit only the derived knob at the fixture boundary.
				script = strings.Replace(script, "  if request['params']['text']=='controlled':", "  value=''\n  if request['params']['text']=='controlled':", 1)
			}
			a, root := attemptTestAdapter(t, script)
			a.providerTransportQualifier = func(core.RuntimeDescriptor) error { return nil }
			r := providerAttempt(root)
			r.Input = "controlled"
			r.Bounds.Duration = 20 * time.Second
			expiry := time.Now().Add(time.Hour)
			switch kind {
			case "turn-expiry":
				r.Bounds.Duration = 2 * time.Second
			case "authority-expiry":
				r.Launch.AuthorityContext.ExpiresAt = time.Now().Add(2 * time.Second)
				r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			case "token-expiry":
				expiry = time.Now().Add(ProviderTokenRefreshMargin + 3*time.Second).Truncate(time.Second)
			}
			a.SetProviderAuthenticationResolver(func(_ context.Context, h core.HermesConfig) (ProviderAccessToken, error) {
				return NewProviderAccessToken(h.Provider, syntheticProviderToken(expiry), time.Now())
			})
			t.Setenv("HERMES_CODEX_EVENT_STALE_TIMEOUT_SECONDS", "999999")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type completed struct {
				result AttemptTurnResult
				err    error
			}
			finished := make(chan completed, 1)
			started := time.Now()
			go func() {
				result, err := a.AttemptTurn(ctx, r)
				finished <- completed{result, err}
			}()
			type handshake struct {
				PID      int    `json:"pid"`
				Timeout  string `json:"timeout"`
				Toolsets string `json:"toolsets"`
				Auth     bool   `json:"auth"`
			}
			type connected struct {
				conn net.Conn
				info handshake
				err  error
			}
			connections := make(chan connected, 1)
			go func() {
				conn, err := listener.Accept()
				var info handshake
				if err == nil {
					_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
					err = json.NewDecoder(conn).Decode(&info)
				}
				connections <- connected{conn, info, err}
			}()
			var peer connected
			select {
			case peer = <-connections:
				if peer.err != nil {
					t.Fatal(peer.err)
				}
				defer peer.conn.Close()
			case done := <-finished:
				t.Fatalf("turn ended before in-flight handshake: %+v %v", done.result.Attempt, done.err)
			case <-time.After(3 * time.Second):
				t.Fatal("gateway did not reach prompt")
			}
			if peer.info.Toolsets != emptyToolset || !peer.info.Auth || len(r.Credentials) != 0 || len(r.Launch.AuthorityContext.Authority.Tools) != 0 || len(r.Launch.AuthorityContext.Authority.Credentials) != 0 {
				t.Fatal("zero-tool/agent-credential isolation changed")
			}
			if kind != "missing-native-knob" {
				seconds, err := strconv.ParseFloat(peer.info.Timeout, 64)
				limit := started.Add(r.Bounds.Duration)
				if r.Launch.AuthorityContext.ExpiresAt.Before(limit) {
					limit = r.Launch.AuthorityContext.ExpiresAt
				}
				if tokenLimit := expiry.Add(-ProviderTokenRefreshMargin); tokenLimit.Before(limit) {
					limit = tokenLimit
				}
				if err != nil || seconds <= 0 || seconds > limit.Sub(started).Seconds() {
					t.Fatalf("watchdog outside approved deadline: %q", peer.info.Timeout)
				}
			}
			if kind == "inflight-cancel" {
				cancel()
			}
			if kind == "quiet-beyond-native-default" {
				// Completion is released only after exceeding the actual native 12s default.
				release := time.NewTimer(12200 * time.Millisecond)
				defer release.Stop()
				select {
				case done := <-finished:
					t.Fatalf("premature timeout during approved quiet stream: %+v %v", done.result.Attempt, done.err)
				case <-release.C:
					if _, err := peer.conn.Write([]byte("!")); err != nil {
						t.Fatal(err)
					}
				}
			}
			var done completed
			select {
			case done = <-finished:
			case <-time.After(15 * time.Second):
				t.Fatal("controller did not terminate/reap bounded stream")
			}
			switch kind {
			case "missing-native-knob":
				if done.err == nil || done.result.Attempt.Reason != "runtime_streaming_timeout" || time.Since(started) < 12*time.Second {
					t.Fatalf("negative control: %+v %v", done.result.Attempt, done.err)
				}
			case "quiet-beyond-native-default":
				if done.err != nil || done.result.Attempt.State != execution.StateSucceeded {
					t.Fatalf("quiet stream: %+v %v", done.result.Attempt, done.err)
				}
			default:
				want := context.DeadlineExceeded
				if kind == "inflight-cancel" {
					want = context.Canceled
				}
				if !errors.Is(done.err, want) || time.Since(started) > 4*time.Second {
					t.Fatalf("late/incorrect interruption: %+v %v", done.result.Attempt, done.err)
				}
				if kind == "authority-expiry" && done.result.Attempt.State != execution.StateExpired {
					t.Fatalf("authority expiry conflated: %+v", done.result.Attempt)
				}
			}
			if err := syscall.Kill(peer.info.PID, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("gateway not reaped: %v", err)
			}
			entries, err := os.ReadDir(filepath.Join(r.StateRoot, "runtime"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("runtime/auth home retained: %v %v", entries, err)
			}
		})
	}
}
