package update

import (
	"context"
	"net/http"
	"os"
	"testing"
)

// Exercise the exact inspection-to-Prepare boundary, including same-byte rename.
func TestBootstrapSealSurvivesPrepare(t *testing.T) {
	for _, attack := range []string{"unchanged", "inode", "inode_during_prepare", "content", "mode", "symlink"} {
		t.Run(attack, func(t *testing.T) {
			u, path := sliceCandidate(t, "")
			info, digest, err := snapshot(path)
			if err != nil {
				t.Fatal(err)
			}
			u.installedInfo, u.installedDigest = info, &digest
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if attack == "inode_during_prepare" {
				original := u.Client.Transport
				if original == nil {
					original = http.DefaultTransport
				}
				u.Client.Transport = bootstrapTransport(func(r *http.Request) (*http.Response, error) {
					if err := os.WriteFile(path+".race", data, info.Mode()); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(path+".race", path); err != nil {
						t.Fatal(err)
					}
					return original.RoundTrip(r)
				})
			}
			switch attack {
			case "inode":
				if err := os.WriteFile(path+".replacement", data, info.Mode()); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(path+".replacement", path); err != nil {
					t.Fatal(err)
				}
			case "content":
				if err := os.WriteFile(path, []byte("changed"), info.Mode()); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
			}
			p, err := u.Prepare(context.Background())
			if p != nil {
				defer p.Close()
			}
			if (err == nil) != (attack == "unchanged") {
				t.Fatalf("attack=%s err=%v", attack, err)
			}
		})
	}
}

type bootstrapTransport func(*http.Request) (*http.Response, error)

func (f bootstrapTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSnapshotBytesSealedDescriptor(t *testing.T) {
	_, path := sliceCandidate(t, "")
	info, data, err := snapshotBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old-aegis" {
		t.Fatalf("wrong snapshot: %q", data)
	}
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("substitution"), 0750); err != nil {
		t.Fatal(err)
	}
	after, _, err := snapshot(path)
	if err != nil || os.SameFile(info, after) || string(data) != "old-aegis" {
		t.Fatal("snapshot bytes lost identity binding")
	}
}
