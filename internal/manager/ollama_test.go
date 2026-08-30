package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoadExternalModelTracksCleanupOwnership(t *testing.T) {
	const name = "exact:model"
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, test := range []struct {
		name          string
		initial       []OllamaModel
		loadStatus    int
		appearOnError bool
		wantOwnership ModelCleanupOwnership
		wantError     bool
		wantLoads     int
	}{
		{name: "pre-existing exact runner is shared", initial: []OllamaModel{{Name: name, Digest: digest}}, loadStatus: http.StatusOK, wantOwnership: ModelCleanupShared, wantLoads: 1},
		{name: "absent successful load is owned", loadStatus: http.StatusOK, wantOwnership: ModelCleanupAegisOwned, wantLoads: 1},
		{name: "ambiguous failed load remains unowned", loadStatus: http.StatusInternalServerError, appearOnError: true, wantOwnership: ModelCleanupUnknown, wantError: true, wantLoads: 1},
		{name: "wrong digest denies before load", initial: []OllamaModel{{Name: name, Digest: "sha256:" + strings.Repeat("b", 64)}}, loadStatus: http.StatusOK, wantOwnership: ModelCleanupUnknown, wantError: true},
		{name: "ambiguous inventory denies before load", initial: []OllamaModel{{Name: name, Digest: digest}, {Model: name, Digest: digest}}, loadStatus: http.StatusOK, wantOwnership: ModelCleanupUnknown, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			running := append([]OllamaModel(nil), test.initial...)
			loads := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/ps":
					_ = json.NewEncoder(writer).Encode(map[string]any{"models": running})
				case "/api/generate":
					loads++
					if test.appearOnError {
						running = []OllamaModel{{Name: name, Digest: digest}}
					}
					if test.loadStatus != http.StatusOK {
						http.Error(writer, "ambiguous load", test.loadStatus)
						return
					}
					if len(running) == 0 {
						running = []OllamaModel{{Name: name, Digest: digest}}
					}
					_ = json.NewEncoder(writer).Encode(map[string]any{"done": true})
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()
			client, err := NewOllamaClient(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			ownership, err := client.LoadExternalModel(context.Background(), name, digest, 65536, time.Minute)
			if (err != nil) != test.wantError || ownership != test.wantOwnership || loads != test.wantLoads {
				t.Fatalf("ownership=%v error=%v loads=%d", ownership, err, loads)
			}
		})
	}
}

func TestUnloadAndVerifySkipsRedundantUnloadWhenModelIsAbsent(t *testing.T) {
	var unloadRequests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/ps" && request.Method == http.MethodGet {
			_ = json.NewEncoder(writer).Encode(map[string]any{"models": []any{}})
			return
		}
		if request.URL.Path == "/api/generate" {
			unloadRequests++
		}
		http.Error(writer, "unexpected request", http.StatusInternalServerError)
	}))
	defer server.Close()
	client, err := NewOllamaClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.UnloadAndVerify(context.Background(), "exact:model", "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if unloadRequests != 0 {
		t.Fatalf("redundant unload requests=%d", unloadRequests)
	}
}

func TestUnloadAndVerifyRequiresExactRunningIdentity(t *testing.T) {
	const name = "exact:model"
	exactDigest := "sha256:" + strings.Repeat("a", 64)
	wrongDigest := "sha256:" + strings.Repeat("b", 64)
	for _, test := range []struct {
		name      string
		inventory []OllamaModel
		wantErr   string
	}{
		{name: "wrong digest", inventory: []OllamaModel{{Name: name, Digest: wrongDigest}}, wantErr: ReasonDigestMismatch},
		{name: "ambiguous", inventory: []OllamaModel{{Name: name, Digest: exactDigest}, {Model: name, Digest: exactDigest}}, wantErr: "ambiguous"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var unloadRequests int
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/api/ps":
					_ = json.NewEncoder(writer).Encode(map[string]any{"models": test.inventory})
				case "/api/generate":
					unloadRequests++
					_ = json.NewEncoder(writer).Encode(map[string]any{"done": true})
				default:
					http.NotFound(writer, request)
				}
			}))
			defer server.Close()
			client, err := NewOllamaClient(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			err = client.UnloadAndVerify(context.Background(), name, exactDigest)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error=%v", err)
			}
			if unloadRequests != 0 {
				t.Fatalf("unsafe unload requests=%d", unloadRequests)
			}
		})
	}
}

func TestUnloadAndVerifyPreservesUnrelatedModelsAndVerifiesExactAbsence(t *testing.T) {
	const name = "exact:model"
	digest := "sha256:" + strings.Repeat("a", 64)
	other := OllamaModel{Name: "operator:model", Digest: "sha256:" + strings.Repeat("c", 64)}
	running := []OllamaModel{{Name: name, Digest: strings.TrimPrefix(digest, "sha256:")}, other}
	var unloadRequests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/ps":
			_ = json.NewEncoder(writer).Encode(map[string]any{"models": running})
		case "/api/generate":
			unloadRequests++
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["model"] != name || body["keep_alive"] != float64(0) {
				t.Errorf("unsafe unload body=%v", body)
			}
			running = []OllamaModel{other}
			_ = json.NewEncoder(writer).Encode(map[string]any{"done": true})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := NewOllamaClient(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.UnloadAndVerify(context.Background(), name, digest); err != nil {
		t.Fatal(err)
	}
	if unloadRequests != 1 || len(running) != 1 || running[0].Name != other.Name {
		t.Fatalf("unloads=%d running=%+v", unloadRequests, running)
	}
}

func TestUnloadAndVerifyRejectsInvalidExpectedDigestBeforeRequest(t *testing.T) {
	client := &OllamaClient{}
	if err := client.UnloadAndVerify(context.Background(), "exact:model", "not-a-digest"); err == nil {
		t.Fatal("invalid digest accepted")
	}
}

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
