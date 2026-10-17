package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPreparedInstallDeniesSubstitutionAndCancellation(t *testing.T) {
	for _, attack := range []string{"target_inode", "target_content", "target_symlink", "candidate_inode", "candidate_content", "cancel"} {
		t.Run(attack, func(t *testing.T) {
			u, path := sliceCandidate(t, "")
			p, err := u.Prepare(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch attack {
			case "target_inode":
				other := path + ".other"
				if err := os.WriteFile(other, []byte("old-aegis"), 0750); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(other, path); err != nil {
					t.Fatal(err)
				}
			case "target_content":
				if err := os.WriteFile(path, []byte("substituted"), 0750); err != nil {
					t.Fatal(err)
				}
			case "target_symlink":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
			case "candidate_inode":
				other := p.candidate + ".other"
				if err := os.WriteFile(other, []byte("new-aegis"), 0750); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(other, p.candidate); err != nil {
					t.Fatal(err)
				}
			case "candidate_content":
				if err := os.WriteFile(p.candidate, []byte("substituted"), 0750); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			}
			before, _ := os.ReadFile(path)
			r, err := p.Install(ctx)
			after, _ := os.ReadFile(path)
			if err == nil || r.Updated || string(before) != string(after) {
				t.Fatalf("result=%+v err=%v before=%q after=%q", r, err, before, after)
			}
		})
	}
}

func TestUpgradeAuthCancellationAfterPreparation(t *testing.T) {
	u, path := sliceCandidate(t, "")
	stopped := false
	_, err := RunUpgrade(context.Background(), u, func(context.Context) (func(context.Context) error, error) {
		candidates, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".aegis-update-*"))
		if len(candidates) != 1 {
			t.Fatalf("authentication preceded candidate preparation: %v", candidates)
		}
		return func(context.Context) error { stopped = true; return nil }, context.Canceled
	})
	contents, _ := os.ReadFile(path)
	if err == nil || stopped || string(contents) != "old-aegis" {
		t.Fatalf("err=%v stopped=%v contents=%q", err, stopped, contents)
	}
}

func TestUpgradeAlreadyCurrentStillUsesLifecycle(t *testing.T) {
	u, path := sliceCandidate(t, "")
	u.CurrentVersion = "1.2.0"
	stopped := false
	r, err := RunUpgrade(context.Background(), u, func(context.Context) (func(context.Context) error, error) {
		return func(context.Context) error { stopped = true; return nil }, nil
	})
	contents, _ := os.ReadFile(path)
	if err != nil || !stopped || r.Updated || r.UpgradeComplete || !r.ResumeRequired || string(contents) != "old-aegis" {
		t.Fatalf("result=%+v err=%v stopped=%v", r, err, stopped)
	}
}
