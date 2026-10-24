package hermes

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/core"
	"github.com/berryhill/aegis/internal/execution"
)

// A real gateway subprocess exercises the launch environment, not a wire knob.
// Missing/disabled native idle policy produces the observed closed timeout code.
const watchdogGateway = `#!/usr/bin/python3
import json, os, select, sys

def emit(value):
 print(json.dumps(value), flush=True)
def event(kind, payload):
 emit({'method':'event','params':{'type':kind,'session_id':'watchdog','payload':payload}})
emit({'method':'event','params':{'type':'gateway.ready','payload':{}}})
for line in sys.stdin:
 request=json.loads(line)
 method=request['method']
 if method=='tools.show':
  emit({'id':request['id'],'result':{'total':0,'sections':[]}})
 elif method=='session.create':
  emit({'id':request['id'],'result':{'session_id':'watchdog'}})
 elif method=='prompt.submit':
  event('message.start',{})
  value=os.environ.get('HERMES_CODEX_EVENT_STALE_TIMEOUT_SECONDS','')
  if not value or float(value)<=0:
   event('message.complete',{'status':'error','error_surface':{'layer':'streaming','code':'timeout'}})
  elif request['params']['text']=='quiet':
   # Model quiet time within the turn; stdin readability interrupts the wait.
   select.select([sys.stdin],[],[],0.04)
   event('message.complete',{'status':'complete','text':value})
  elif request['params']['text']=='wait':
   # No completion: controller deadline/cancellation must kill and reap us.
   select.select([sys.stdin],[],[])
  else:
   event('message.complete',{'status':'complete','text':value})
`

func TestCodexAttemptDerivedWatchdog(t *testing.T) {
	for _, kind := range []string{"turn", "authority", "token", "caller", "quiet", "duration", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			a, root := attemptTestAdapter(t, watchdogGateway)
			a.providerTransportQualifier = func(core.RuntimeDescriptor) error { return nil }
			r := providerAttempt(root)
			r.Bounds.Duration = 3 * time.Second
			expiry := time.Now().Add(time.Hour).Truncate(time.Second)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			limit := r.Bounds.Duration
			switch kind {
			case "authority":
				limit = 2 * time.Second
				r.Launch.AuthorityContext.ExpiresAt = time.Now().Add(limit)
				r.Launch.AuthorityContext.Digest = core.AuthorityContextDigest(r.Launch.AuthorityContext)
			case "token":
				expiry = time.Now().Add(ProviderTokenRefreshMargin + 2*time.Second).Truncate(time.Second)
				limit = time.Until(expiry.Add(-ProviderTokenRefreshMargin))
			case "caller":
				limit = 2 * time.Second
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, limit)
				defer stop()
			case "quiet":
				r.Input = "quiet"
			case "duration":
				r.Input = "wait"
				r.Bounds.Duration = 200 * time.Millisecond
			case "cancel":
				r.Input = "wait"
				cancel()
			}
			a.SetProviderAuthenticationResolver(func(_ context.Context, h core.HermesConfig) (ProviderAccessToken, error) {
				return NewProviderAccessToken(h.Provider, syntheticProviderToken(expiry), time.Now())
			})
			t.Setenv("HERMES_CODEX_EVENT_STALE_TIMEOUT_SECONDS", "999999")
			started := time.Now()
			result, err := a.AttemptTurn(ctx, r)
			if kind == "duration" || kind == "cancel" {
				want := context.DeadlineExceeded
				if kind == "cancel" {
					want = context.Canceled
				}
				if !errors.Is(err, want) {
					t.Fatalf("state=%s reason=%s err=%v", result.Attempt.State, result.Attempt.Reason, err)
				}
				if time.Since(started) > 2*time.Second {
					t.Fatal("process not promptly reaped")
				}
			} else {
				if err != nil {
					t.Fatalf("state=%s reason=%s err=%v", result.Attempt.State, result.Attempt.Reason, err)
				}
				seconds, e := strconv.ParseFloat(result.Output, 64)
				if e != nil || seconds <= 0 || seconds > limit.Seconds() {
					t.Fatalf("timeout %q outside (0,%s]", result.Output, limit)
				}
			}
			entries, e := os.ReadDir(filepath.Join(r.StateRoot, "runtime"))
			if kind == "cancel" && os.IsNotExist(e) {
				return
			}
			if e != nil || len(entries) != 0 {
				t.Fatalf("runtime/auth home retained: %v %v", entries, e)
			}
		})
	}
}

func TestNonCodexAttemptHasNoWatchdogOverride(t *testing.T) {
	a, root := attemptTestAdapter(t, strings.Replace(watchdogGateway, "value=os.environ.get('HERMES_CODEX_EVENT_STALE_TIMEOUT_SECONDS','')", "value='1' if 'HERMES_CODEX_EVENT_STALE_TIMEOUT_SECONDS' not in os.environ else ''", 1))
	t.Setenv("HERMES_CODEX_EVENT_STALE_TIMEOUT_SECONDS", "999999")
	result, err := a.AttemptTurn(context.Background(), validAttemptTurnRequest(root))
	if err != nil || result.Attempt.State != execution.StateSucceeded {
		t.Fatalf("override leaked: %+v %v", result, err)
	}
}
