package update

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

type upgradeBinary struct{ applied bool }

func (b *upgradeBinary) Run(_ context.Context, check bool) (Result, error) {
	if !check {
		b.applied = true
	}
	return Result{}, nil
}
func TestUpgradePhases(t *testing.T) {
	for _, name := range []string{"healthy", "already_stopped", "unavailable_auth", "cancelled_auth", "identity_failure", "stop_failure"} {
		t.Run(name, func(t *testing.T) {
			b, path := sliceCandidate(t, "")
			stopped := false
			boundary := func(ctx context.Context) (func(context.Context) error, error) {
				switch name {
				case "unavailable_auth", "identity_failure":
					return nil, errors.New(name)
				case "cancelled_auth":
					return nil, context.Canceled
				}
				return func(context.Context) error {
					if name == "stop_failure" {
						return errors.New(name)
					}
					stopped = true
					return nil
				}, nil
			}
			r, err := RunUpgrade(context.Background(), b, boundary)
			contents, _ := os.ReadFile(path)
			applied := bytes.Equal(contents, candidateFixture(t, "1.2.0"))
			good := name == "healthy" || name == "already_stopped"
			if applied != good || stopped != good || (err == nil) != good || r.UpgradeComplete || !r.ResumeRequired {
				t.Fatalf("result=%+v err=%v applied=%v stopped=%v", r, err, applied, stopped)
			}
			if name == "cancelled_auth" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}
