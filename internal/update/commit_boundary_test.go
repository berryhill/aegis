package update

import (
	"context"
	"os"
	"testing"
)

func TestPreparedCommitBoundary(t *testing.T) {
	for _, attack := range []string{"candidate", "target", "cancel"} {
		t.Run(attack, func(t *testing.T) {
			u, path := sliceCandidate(t, "")
			p, err := u.Prepare(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p.beforeCommit = func() {
				if attack == "cancel" {
					cancel()
					return
				}
				destination := p.candidate
				if attack == "target" {
					destination = path
				}
				other := destination + ".substitution"
				if err := os.WriteFile(other, []byte("substitution"), 0750); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(other, destination); err != nil {
					t.Fatal(err)
				}
			}
			r, err := p.Install(ctx)
			if err == nil || r.Updated {
				t.Fatalf("commit admitted %s: updated=%v err=%v", attack, r.Updated, err)
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := "old-aegis"
			if attack == "target" {
				want = "substitution"
			}
			if string(contents) != want {
				t.Fatal("denial replaced the target")
			}
		})
	}
}
