package implementation

import (
	"context"
	"fmt"
	"github.com/berryhill/aegis/internal/loop"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNativeToolchainComponentIdentity(t *testing.T) {
	ctx := context.Background()
	kernel, c := fixture(t)
	base := t.TempDir()
	if err := os.Chmod(base, 0700); err != nil {
		t.Fatal(err)
	}
	files := []string{"bin/go", "VERSION", "go.env", "src/runtime/runtime.go", "pkg/tool/" + runtime.GOOS + "_" + runtime.GOARCH + "/compile", "pkg/tool/" + runtime.GOOS + "_" + runtime.GOARCH + "/link", "pkg/include/textflag.h", "lib/time/zoneinfo.zip"}
	for _, p := range files {
		p = filepath.Join(base, p)
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("isolated fake component"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	e := &ManagedContribution{DB: kernel.DB, GoBinary: filepath.Join(base, "bin/go")}
	if _, err := e.verifyToolchain(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range files {
		t.Run(p, func(t *testing.T) {
			p = filepath.Join(base, p)
			old, _ := os.ReadFile(p)
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			// Equal size and restored mtime must not hide substituted bytes.
			replacement := append([]byte(nil), old...)
			replacement[0] ^= 0xff
			if err := os.WriteFile(p, replacement, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if _, err := e.verifyToolchain(ctx); err == nil {
				t.Fatal("component substitution accepted")
			}
			os.WriteFile(p, old, 0600)
		})
	}
	p := filepath.Join(base, "src/runtime/runtime.go")
	old, _ := os.ReadFile(p)
	os.Remove(p)
	os.Symlink(filepath.Join(base, "VERSION"), p)
	if _, err := e.verifyToolchain(ctx); err == nil {
		t.Fatal("symlink accepted")
	}
	os.Remove(p)
	os.WriteFile(p, old, 0600)
	os.Chmod(p, 0666)
	if _, err := e.verifyToolchain(ctx); err == nil {
		t.Fatal("unsafe input accepted")
	}
	os.Chmod(p, 0600)
	os.Remove(p)
	if _, err := e.verifyToolchain(ctx); err == nil {
		t.Fatal("missing input accepted")
	}
	if err := os.WriteFile(p, old, 0600); err != nil {
		t.Fatal(err)
	}
	t.Run("native_pre_and_post_execution", func(t *testing.T) {
		// Synthetic executable: boundary ordering, not Go qualification.
		marker := filepath.Join(t.TempDir(), "executed")
		script := fmt.Sprintf("#!/bin/sh\nprintf ran > %q\nprintf substituted > %q\n", marker, filepath.Join(base, "VERSION"))
		if err := os.WriteFile(e.GoBinary, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(e.GoBinary, 0700); err != nil {
			t.Fatal(err)
		}
		e.toolchainDigest = ""
		e.StateRoot, e.Admit = t.TempDir(), kernel.Admit
		if _, err := e.verifyToolchain(ctx); err != nil {
			t.Fatal(err)
		}
		source, err := Inventory(ctx, c.Workspace)
		if err != nil {
			t.Fatal(err)
		}
		component := filepath.Join(base, "VERSION")
		original, err := os.ReadFile(component)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(component, []byte("pre-execution drift"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := e.native(ctx, c, source, nil); err == nil {
			t.Fatal("pre-execution drift accepted")
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("substituted toolchain executed: %v", err)
		}
		if err := os.WriteFile(component, original, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := e.native(ctx, c, source, nil); err == nil {
			t.Fatal("post-execution drift accepted")
		}
		if _, err := os.Stat(marker); err != nil {
			t.Fatalf("post-execution boundary was not reached: %v", err)
		}
	})
}

func TestCandidateReviewGate(t *testing.T) {
	kernel, _ := fixture(t)
	p := loop.ContributionExecutionPolicy{Kind: "go-stdlib-behavior.v1", RepositoryPath: t.TempDir(), GitBinary: "/usr/bin/git", BaseBranch: "main", BuildPackage: "."}
	for _, kind := range []string{"behavior", "security", "launch", "denial"} {
		p.Criteria = append(p.Criteria, loop.ContributionCriterion{ID: kind, Kind: kind, Test: loop.RequiredGoTest{Package: "fixture", Name: "Test" + kind}})
	}
	v := ContributionCandidate{Digest: "candidate", Source: SourceInventory{Digest: "source", Files: []SourceFile{{Path: "SECURITY.md", Content: []byte("reviewed security")}, {Path: "README.md", Content: []byte("reviewed launch")}, {Path: "generated.txt", Content: []byte("reviewed generated")}}}}
	for i, f := range v.Source.Files {
		scope := "launch"
		if i == 0 {
			scope = "security"
		}
		d := digest(f.Content)
		baseline := d
		if i == 2 {
			baseline = digest([]byte("old generated"))
		}
		p.ReviewAssets = append(p.ReviewAssets, loop.ContributionReviewAsset{ID: f.Path, Path: f.Path, Scope: scope, BaselineDigest: baseline, ApprovedDigest: d})
	}
	p.ReviewAssets = append(p.ReviewAssets, loop.ContributionReviewAsset{ID: "release", Scope: "launch", NonApplicable: "This controlled local fixture has no published release assets."})
	v.Policy = contributionPolicyDigest(p)
	e := &ManagedContribution{DB: kernel.DB}
	d, err := e.persistReview(v, p)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.validateReview(v, p, d); err != nil {
		t.Fatal(err)
	}
	l, err := reviewCandidate(v, p)
	if err != nil || l.Assets[0].Status != "unaffected" || l.Assets[2].Status != "changed" {
		t.Fatalf("ledger: %+v %v", l, err)
	}
	for _, mutation := range []string{"missing-ledger", "caller-PASS", "wrong-source", "wrong-policy", "missing-asset", "docs-mutation", "generated-mutation", "builder-substitution"} {
		t.Run(mutation, func(t *testing.T) {
			x := v
			x.Source.Files = append([]SourceFile(nil), v.Source.Files...)
			q := p
			ledger := d
			switch mutation {
			case "missing-ledger":
				ledger = ""
			case "caller-PASS":
				ledger = "PASS"
			case "wrong-source":
				x.Source.Digest = "other"
			case "wrong-policy":
				x.Policy = "other"
			case "missing-asset":
				x.Source.Files = x.Source.Files[:1]
			case "docs-mutation":
				x.Source.Files[1].Content = []byte("PASS")
			case "generated-mutation":
				x.Source.Files[2].Content = []byte("new output")
			case "builder-substitution":
				x.Source.Files[0].Content = []byte("builder PASS")
			}
			if e.validateReview(x, q, ledger) == nil {
				t.Fatal("untrusted review accepted")
			}
		})
	}
	p.ReviewAssets = nil
	if p.Validate() == nil {
		t.Fatal("missing review policy accepted")
	}
}
