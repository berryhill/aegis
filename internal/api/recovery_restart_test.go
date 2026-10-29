package api

import (
	"context"
	"encoding/json"
	"github.com/berryhill/aegis/internal/evidence"
	"github.com/berryhill/aegis/internal/orchestration"
	fleetbadger "github.com/berryhill/aegis/internal/persistence/fleet/badger"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
	authoritybadger "github.com/berryhill/aegis/internal/persistence/authority/badger"
	"github.com/berryhill/aegis/internal/runtime/hermes"
	"github.com/berryhill/aegis/internal/store"
)

type recoveryRestartFixture struct {
	Config   config.Config
	Selector app.RequeueQueueItemInput
	Expected app.QueueExecutionView
	Success  bool
}

// The original owning service has stopped before closing its real stores. A
// separate test executable process reopens both and serves fresh peer authority.
func recoveryServiceRestart(t *testing.T, s *app.Service, closeFleet func(), selector app.RequeueQueueItemInput, expected app.QueueExecutionView, success bool) {
	t.Helper()
	closeFleet()
	if err := s.Authority.(interface{ Close() error }).Close(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(recoveryRestartFixture{s.Config, selector, expected, success})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "restart.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestRecoveryOwningServiceRestartChild$", "-test.v")
	cmd.Env = append(os.Environ(), "AEGIS_RECOVERY_RESTART="+path)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatal("owning service restart", err)
	}
}

func TestRecoveryOwningServiceRestartChild(t *testing.T) {
	path := os.Getenv("AEGIS_RECOVERY_RESTART")
	if path == "" {
		t.Skip("child-only durable service restart")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f recoveryRestartFixture
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state, err := store.Open(f.Config.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := authoritybadger.Open(ctx, filepath.Join(f.Config.StateDir, "persistence", "authority-v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := app.New(f.Config, state, authority, authority, hermes.New(f.Config.HermesExecutable, logger), logger)
	configureAPIFleet(t, s)
	if err = s.QueueWorker.ConfigureImplementation(f.Config.Implementation, s.Config.StateDir, s.Hermes); err != nil {
		t.Fatal(err)
	}
	cliSvc := *s
	cliSvc.Config = s.Config
	cliSvc.Config.Credentials.Authority = config.CredentialAuthority{}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, s) }()
	defer func() {
		cancel()
		if x := <-done; x != nil {
			t.Error(x)
		}
	}()
	waitFor(t, "unix", s.Config.API.UnixSocket)
	cli := newInstalledLoopQueueClient(t, &cliSvc)
	var got app.QueueExecutionView
	if err = cli.run(&got, "queue", "show", f.Expected.Item.ItemID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, f.Expected) {
		t.Fatal("durable CLI readback changed")
	}
	client := unixClient(s.Config.API.UnixSocket)
	if f.Success {
		var result app.QueueLoopResult
		apiRequest(t, client, http.MethodPost, "/v1/queue/"+f.Selector.QueueItem.ID+"/requeue", f.Selector, &result, http.StatusOK)
		if result.Execution == nil || result.QueueItemID != f.Expected.Item.ItemID || !reflect.DeepEqual(*result.Execution, f.Expected) {
			t.Fatal("same-intent durable reconciliation changed result")
		}
	} else {
		var denial struct {
			Code string `json:"code"`
		}
		apiRequest(t, client, http.MethodPost, "/v1/queue/"+f.Selector.QueueItem.ID+"/requeue", f.Selector, &denial, http.StatusConflict)
		if denial.Code != "native_recovery_effect_evidence_missing" {
			t.Fatal(denial)
		}
	}
	t.Log("separate-process owning-service restart: actual CLI exact history and authenticated same-intent recovery preserved")
}

// Recovery fixtures close once before a child takes store ownership.
func configureRecoveryAPIFleet(t *testing.T, svc *app.Service) func() {
	t.Helper()
	fs, err := fleetbadger.Open(context.Background(), filepath.Join(svc.Config.StateDir, "persistence", "fleet-v1"))
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	closeStore := func() {
		once.Do(func() {
			if err := fs.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(closeStore)
	fleetService, err := orchestration.NewFleetService(fs, svc.Authority, svc.AuthorityCommands, svc.AuthorizeFleetAction, nil, svc.Now)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := evidence.NewBlobVerifier(svc.Store)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := orchestration.NewQueueWorker(fs, fleetService, svc.Store, verifier, orchestration.NoKeyAdapter{}, svc.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err = configureSyntheticAPIFleet(svc, fs, fleetService, worker); err != nil {
		t.Fatal(err)
	}
	return closeStore
}
