package initialize

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/berryhill/aegis/internal/config"
)

func testService(t *testing.T) *Service {
	t.Helper()
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	copy := *current
	return &Service{
		Current: func() (*user.User, error) { value := copy; return &value, nil },
		LookupID: func(uid string) (*user.User, error) {
			if uid != copy.Uid {
				return nil, errors.New("unexpected uid")
			}
			value := copy
			return &value, nil
		},
	}
}

func TestCancelledApplyLeavesNoAcceptedPartialConfiguration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "aegis.yaml")
	service := testService(t)
	plan, err := service.Plan(path, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = service.Apply(ctx, plan); !errors.Is(err, context.Canceled) {
		t.Fatalf("apply error=%v", err)
	}
	inspection := config.Inspect(path)
	if inspection.State != config.StateAbsent {
		t.Fatalf("cancelled apply state=%s error=%v", inspection.State, inspection.Err)
	}
}

func TestApplyRefusesConfigurationCreatedAfterPreview(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "aegis.yaml")
	service := testService(t)
	plan, err := service.Plan(path, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	original := []byte("not the planned configuration\n")
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err = service.Apply(context.Background(), plan); err == nil {
		t.Fatal("configuration created after preview was overwritten")
	}
	contents, readErr := os.ReadFile(path)
	if readErr != nil || string(contents) != string(original) {
		t.Fatalf("configuration changed: %q %v", contents, readErr)
	}
}

func TestPlanRejectsAmbiguousHostIdentity(t *testing.T) {
	service := testService(t)
	service.LookupID = func(string) (*user.User, error) {
		return &user.User{Uid: "999999", Username: "different"}, nil
	}
	if _, err := service.Plan(filepath.Join(t.TempDir(), "aegis.yaml"), ""); err == nil {
		t.Fatal("ambiguous host identity was accepted")
	}
}
