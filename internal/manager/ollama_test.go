package manager

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOllamaPullStreamsProgressAndRequiresSuccess(t *testing.T) {
	for _, test := range []struct {
		name, stream string
		wantErr      string
	}{
		{name: "success", stream: "{\"status\":\"pulling manifest\"}\n{\"status\":\"downloading\",\"total\":100,\"completed\":42}\n{\"status\":\"success\"}\n"},
		{name: "truncated", stream: "{\"status\":\"downloading\",\"total\":100,\"completed\":42}\n", wantErr: "without a success event"},
		{name: "malformed", stream: "not-json\n", wantErr: "malformed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/api/pull" || request.Method != http.MethodPost {
					t.Errorf("request=%s %s", request.Method, request.URL.Path)
				}
				writer.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = fmt.Fprint(writer, test.stream)
			}))
			defer server.Close()
			client, err := NewOllamaClient(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			var events []PullProgress
			err = client.Pull(context.Background(), "approved:model", func(event PullProgress) { events = append(events, event) })
			if test.wantErr == "" && (err != nil || len(events) != 3) {
				t.Fatalf("events=%+v err=%v", events, err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
