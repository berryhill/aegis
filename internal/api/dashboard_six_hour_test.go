package api

import (
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// Real listener -> password login -> CSRF/review -> durable credential service
// effect. Merely widening cookie or manager admission must not pass this test.
func TestDashboardSixHourActionAndReadBoundary(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	var elapsed atomic.Int64
	f := newCredentialRouteFixtureWithClock(t, 6*time.Hour, func() time.Time { return start.Add(time.Duration(elapsed.Load())) })
	for _, offset := range []time.Duration{15 * time.Minute, time.Hour, 6*time.Hour - time.Nanosecond} {
		elapsed.Store(int64(offset))
		response, err := f.client.Get("http://" + f.address + "/console/api/state")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK || len(response.Cookies()) != 0 {
			t.Fatalf("read at %s: status=%d", offset, response.StatusCode)
		}
		receipt := f.review(url.Values{"operation": {"backup"}})
		f.execute(receipt, http.StatusOK)
		if info, err := os.Stat(f.svc.Config.Credentials.Authority.Database + ".backup"); err != nil || info.Size() == 0 {
			t.Fatal("actual backup effect missing")
		}
		// A backup destination is create-only. Remove this isolated test output so
		// the next boundary proves a new effect rather than an overwrite denial.
		if err := os.Remove(f.svc.Config.Credentials.Authority.Database + ".backup"); err != nil {
			t.Fatal(err)
		}
	}
	for _, offset := range []time.Duration{6 * time.Hour, 6*time.Hour + time.Nanosecond} {
		elapsed.Store(int64(offset))
		response, err := f.client.Get("http://" + f.address + "/console/api/state")
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expired read at %s: %d", offset, response.StatusCode)
		}
		response, _ = f.post(f.client, "/console/credentials/operation/review", url.Values{"csrf": {f.csrf}, "operation": {"backup"}})
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("expired action at %s: %d", offset, response.StatusCode)
		}
	}
}
