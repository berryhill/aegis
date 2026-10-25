//go:build linux

package config

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const nextNativeContract = "    - sha256:7be4f14b75128b7e582caf5d176d7314a6c26ed33dd8c757ff8189531703f95c\n"

// Real connection publication and replay, not a mocked history validator.
func TestNativeTrustSuccessiveExactAdditions(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprint("existing-block=", initial), func(t *testing.T) {
			u, err := user.Current()
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "owner.yaml")
			raw := []byte("principal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n")
			if initial {
				raw = append(raw, []byte(nativeTrustBlock)...)
			}
			write := func(b []byte) {
				t.Helper()
				if err := os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write(raw)
			binding := ControllerCodexLogin{ProfileHome: filepath.Join(u.HomeDir, ".hermes"), AccountID: "synthetic-account", ApprovedBy: "operator"}
			if err := ConnectControllerCodex(path, raw, binding); err != nil {
				t.Fatal(err)
			}
			retained := map[string][]byte{}
			for generation := 1; generation <= 3; generation++ {
				if generation == 1 && !initial {
					raw = append(raw, []byte(nativeTrustBlock)...)
				} else {
					line := strings.Replace(nextNativeContract, "7be4f14b", fmt.Sprintf("%08d", generation), 1)
					raw = append(raw, []byte(line)...)
				}
				write(raw)
				if _, err := ReadControllerCodexBinding(path); err == nil {
					t.Fatal("runtime accepted unreviewed config")
				}
				prev, review, err := ReadControllerCodexForReview(path)
				if err != nil || prev == nil || *prev != binding || !review.Additive {
					t.Fatalf("generation %d review: %v", generation, err)
				}
				if err := ReplaceControllerCodexReviewed(path, raw, binding, binding, review); err != nil {
					t.Fatalf("generation %d apply: %v", generation, err)
				}
				if b, err := ReadControllerCodexBinding(path); err != nil || b == nil || *b != binding {
					t.Fatalf("generation %d readback: %v", generation, err)
				}
				for name, old := range retained {
					got, err := os.ReadFile(name)
					if err != nil || !bytes.Equal(got, old) {
						t.Fatal("history rewritten")
					}
				}
				name := fmt.Sprintf("%s%s.replacement-%06d", path, connectionSuffix, generation)
				retained[name], err = os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestNativeTrustExistingBlockDeltaDenials(t *testing.T) {
	old := []byte("principal:\n  id: operator\n" + nativeTrustBlock)
	for name, raw := range map[string][]byte{
		"single":         append(append([]byte{}, old...), []byte(nextNativeContract)...),
		"two":            append(append([]byte{}, old...), []byte(nextNativeContract+strings.ReplaceAll(nextNativeContract, "7be4f14b", "8be4f14b"))...),
		"duplicate":      append(append([]byte{}, old...), []byte("    - sha256:67b258b972d3264b4e93f2bacfac500c5c52a7a9815322e2aefb475674a7da58\n")...),
		"changed-go":     bytes.ReplaceAll(append(append([]byte{}, old...), []byte(nextNativeContract)...), []byte("/synthetic/go"), []byte("/other/go")),
		"wildcard":       append(append([]byte{}, old...), []byte("    - '*'\n")...),
		"removed":        bytes.ReplaceAll(old, []byte("    - sha256:67b258b972d3264b4e93f2bacfac500c5c52a7a9815322e2aefb475674a7da58\n"), nil),
		"unrelated":      append(append(append([]byte{}, old...), []byte(nextNativeContract)...), []byte("# unrelated\n")...),
		"duplicate-yaml": append(append(append([]byte{}, old...), []byte(nextNativeContract)...), []byte("  go_binary: /other/go\n")...),
	} {
		t.Run(name, func(t *testing.T) {
			got := nativeTrustAddition(raw, connectionDigest(old))
			if got != (name == "single") {
				t.Fatalf("delta accepted=%v", got)
			}
		})
	}
}

func TestNativeTrustCompetingExactAdditionCommits(t *testing.T) {
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "owner.yaml")
	old := []byte("principal:\n  id: operator\n  uid: '" + u.Uid + "'\n  user: '" + u.Username + "'\n" + nativeTrustBlock)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	b := ControllerCodexLogin{ProfileHome: filepath.Join(u.HomeDir, ".hermes"), AccountID: "synthetic-account", ApprovedBy: "operator"}
	if err := ConnectControllerCodex(path, old, b); err != nil {
		t.Fatal(err)
	}
	raw := append(append([]byte{}, old...), []byte(nextNativeContract)...)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, review, err := ReadControllerCodexForReview(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- ReplaceControllerCodexReviewed(path, raw, b, b, review) })
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("winners=%d", successes)
	}
	if _, err := ReadControllerCodexBinding(path); err != nil {
		t.Fatal(err)
	}
}
