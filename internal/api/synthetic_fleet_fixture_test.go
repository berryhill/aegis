package api

import (
	"bytes"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/config"
)

// Setup only: no Serve, worker execution, or installed runtime launch.
func TestSyntheticFleetFixtureHermeticity(t *testing.T) {
	repository, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	operator, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(operator.HomeDir, repository)
	if err != nil || rel == ".." || filepath.IsAbs(rel) || len(rel) >= 3 && rel[:3] == "../" {
		t.Fatal("repository-local fixture requires operator-home ancestry")
	}
	root, err := os.MkdirTemp(repository, ".synthetic-fleet-*")
	if err != nil {
		t.Fatal(err)
	}
	// Only this newly allocated fixture tree is removed, after store cleanups.
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Error(err)
		}
	})
	info, err := os.Stat(root)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("fixture root must be private: info=%v err=%v", info, err)
	}
	var previous *app.Service
	for _, name := range []string{"first", "second"} {
		svc := apiServiceAt(t, filepath.Join(root, name))
		original := svc.Hermes
		before, err := json.Marshal(svc.Config)
		if err != nil {
			t.Fatal(err)
		}
		fleetStore := configureAPIFleet(t, svc)
		if original == nil || svc.Hermes != original {
			t.Error("configureAPIFleet did not restore the exact original Hermes pointer")
		}
		after, err := json.Marshal(svc.Config)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Error("configureAPIFleet changed synthetic Config bytes")
		}
		runtimePath := filepath.Join(svc.Config.StateDir, "runtime")
		if _, err := os.Lstat(runtimePath); !os.IsNotExist(err) {
			t.Errorf("synthetic setup leaked runtime directory: %s (err=%v); installed discovery prerequisite present when directory exists", runtimePath, err)
		}
		if fleetStore == nil || svc.FleetRepository != fleetStore || svc.QueueWorker == nil {
			t.Fatal("fixture fleet wiring missing")
		}
		if previous != nil && (svc.Store == previous.Store || svc.FleetRepository == previous.FleetRepository || svc.Fleet == previous.Fleet || svc.QueueWorker == previous.QueueWorker || svc.Hermes == previous.Hermes || svc.Config.StateDir == previous.Config.StateDir) {
			t.Error("independent fixtures share stores, services, workers, adapter or state")
		}
		fake := filepath.Join(root, name, "fake-implementation")
		if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
			t.Fatal(err)
		}
		fakeHome := filepath.Join(root, name, "fake-laya-home")
		if err := os.Mkdir(fakeHome, 0700); err != nil {
			t.Fatal(err)
		}
		if err := svc.QueueWorker.ConfigureImplementation(config.Implementation{GoBinary: fake, LayaPython: fake, LayaHome: fakeHome}, svc.Config.StateDir, original); err != nil {
			t.Fatalf("later explicit fake implementation with original Hermes: %v", err)
		}
		if svc.Hermes != original {
			t.Error("explicit implementation replaced original Hermes")
		}
		previous = svc
	}
}
