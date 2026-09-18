package command

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/berryhill/aegis/internal/config"
	"github.com/spf13/cobra"
)

func TestOnlineProcessTimeoutIsUnknownNotUnreachable(t *testing.T) {
	root := t.TempDir()
	cfg := config.Defaults()
	cfg.StateDir = filepath.Join(root, "unopened")
	cfg.API.UnixSocket = filepath.Join(root, "api.sock")
	cfg.API.Token = "synthetic"
	cfg.Principal.UID = "1000"
	cfg.Principal.User = "synthetic"
	listener, err := net.Listen("unix", cfg.API.UnixSocket)
	if err != nil {
		t.Fatal(err)
	}
	var dispatched atomic.Int32
	var delayed atomic.Bool
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/config" {
			json.NewEncoder(w).Encode(config.Redacted(cfg))
			return
		}
		dispatched.Add(1)
		if delayed.Load() {
			time.Sleep(80 * time.Millisecond)
			io.WriteString(w, `{}`)
			return
		}
		<-r.Context().Done()
	})}
	go server.Serve(listener)
	defer server.Close()
	raw, _ := json.Marshal(cfg)
	configFile := filepath.Join(root, "config.json")
	os.WriteFile(configFile, raw, 0600)
	input := filepath.Join(root, "input.json")
	os.WriteFile(input, []byte(`{"queue_item_id":"exact-item"}`), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd := NewRoot(Dependencies{Out: io.Discard, Err: io.Discard})
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--config", configFile, "--target", cfg.API.Console.Origin, "queue", "process", input})
	err = cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "outcome_unknown") || !strings.Contains(err.Error(), "exact-item") {
		t.Fatalf("wrong timeout classification: %v", err)
	}
	if dispatched.Load() != 1 {
		t.Fatalf("dispatch count %d", dispatched.Load())
	}
	delayed.Store(true)
	parent := &cobra.Command{Use: "queue"}
	process := &cobra.Command{Use: "process"}
	parent.AddCommand(process)
	process.SetContext(context.Background())
	process.SetOut(io.Discard)
	if err := runFleetOnlineWithTimeouts(process, []string{input}, &rootOptions{configFile: configFile, target: cfg.API.Console.Origin}, 40*time.Millisecond, time.Second); err != nil {
		t.Fatalf("execution used control deadline: %v", err)
	}
	if dispatched.Load() != 2 {
		t.Fatalf("unexpected retries: %d", dispatched.Load())
	}
	for _, tc := range []struct {
		parent, name string
		args         []string
	}{
		{"aegis", "provision", []string{"exact-plan", "exact-approval"}},
		{"session", "start", []string{"exact-mandate"}},
	} {
		parent := &cobra.Command{Use: tc.parent}
		child := &cobra.Command{Use: tc.name}
		parent.AddCommand(child)
		child.SetContext(context.Background())
		child.SetOut(io.Discard)
		if err := runFleetOnlineWithTimeouts(child, tc.args, &rootOptions{configFile: configFile, target: cfg.API.Console.Origin}, 40*time.Millisecond, time.Second); err != nil {
			t.Fatalf("%s used control deadline: %v", tc.name, err)
		}
		if err := runFleetOnlineWithTimeouts(child, tc.args, &rootOptions{configFile: configFile, target: cfg.API.Console.Origin}, time.Second, 20*time.Millisecond); err == nil || !strings.Contains(err.Error(), "outcome_unknown") {
			t.Fatalf("%s timeout classification: %v", tc.name, err)
		}
	}
	if dispatched.Load() != 6 {
		t.Fatalf("unexpected preparation retries: %d", dispatched.Load())
	}
}
