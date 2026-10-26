package delivery

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// Synthetic known-pattern canaries only; never print fixture contents.
// This reproduces the independent tester's actual Engine -> closed HTTP path.
func TestPublicationEncodedAssignmentEgress(t *testing.T) {
	canary := []byte("ghp_" + strings.Repeat("A", 36))
	encodings := map[string]func([]byte) string{
		"standard": base64.StdEncoding.EncodeToString,
		"raw":      base64.RawStdEncoding.EncodeToString,
		"url":      base64.URLEncoding.EncodeToString,
		"raw-url":  base64.RawURLEncoding.EncodeToString,
		"hex":      hex.EncodeToString,
		"two-layers": func(b []byte) string {
			return base64.StdEncoding.EncodeToString([]byte("inner=" + hex.EncodeToString(b)))
		},
	}
	wrappers := map[string]func(string) string{
		"unquoted":  func(s string) string { return "fixture=" + s },
		"quoted":    func(s string) string { return "fixture=\"" + s + "\"" },
		"json":      func(s string) string { return "{\"fixture\":\"" + s + "\"}" },
		"shell":     func(s string) string { return "export fixture='" + s + "'" },
		"append":    func(s string) string { return "fixture+=" + s },
		"adjacent":  func(s string) string { return "first=c2FmZSBwdWJsaWMgdGV4dA==;fixture=" + s + ",last=ok" },
		"long-name": func(s string) string { return strings.Repeat("field", 16) + "=" + s },
	}
	for encoding, encode := range encodings {
		for wrapper, wrap := range wrappers {
			t.Run(encoding+"/"+wrapper, func(t *testing.T) {
				p := publicationPlan(t)
				publicationRegraph(t, &p, []string{"session.go"}, [][]byte{[]byte(wrap(encode(canary)))})
				i, e := intentFor(p, Publish)
				if e != nil {
					t.Fatal(e)
				}
				if !errors.Is((PublicationPrivacy{}).Review(context.Background(), i, p), ErrPublicationContent) {
					t.Fatal("encoded known-pattern content was not denied")
				}
				state := &objectServer{}
				g := serveObjects(t, p, state)
				custody := &publicationTestCustody{}
				g.privacy = PublicationPrivacy{}
				g.custody = custody
				counter := &publicationHTTPCount{next: g.client.Transport}
				g.client.Transport = counter
				engine := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, g)
				r, _ := engine.Execute(context.Background(), p.Reference, Publish)
				if r.Outcome != Denied || custody.calls != 0 || counter.calls.Load() != 0 || len(state.writes) != 0 || state.ref {
					t.Fatal("encoded denial reached custody or HTTP")
				}
			})
		}
	}
	// Safe assignment/encoding is not authority denial: real Review and HTTP succeed.
	t.Run("safe-publication", func(t *testing.T) {
		p := publicationPlan(t)
		body := "export fixture=" + base64.StdEncoding.EncodeToString([]byte("safe public documentation")) + "\ncount=42\nvar token = generateToken()\n"
		publicationRegraph(t, &p, []string{"session.go"}, [][]byte{[]byte(body)})
		state := &objectServer{}
		g := serveObjects(t, p, state)
		custody := &publicationTestCustody{}
		g.privacy = PublicationPrivacy{}
		g.custody = custody
		counter := &publicationHTTPCount{next: g.client.Transport}
		g.client.Transport = counter
		engine := NewEngine(&memoryOwner{}, fixtureSource{p}, &fixtureAuthority{}, g)
		r, e := engine.Execute(context.Background(), p.Reference, Publish)
		if e != nil || r.Outcome != Confirmed || custody.calls == 0 || counter.calls.Load() == 0 || !state.ref || len(state.writes) != len(p.Objects) {
			t.Fatal("safe encoded publication failed", e)
		}
	})
}

func TestPublicationEncodingBounds(t *testing.T) {
	// Include actual '+'/'/' and URL '-'/'_' alphabets, with both padding lengths.
	for _, prefix := range []string{"", "~", "~~", "???", "~~~"} {
		raw := []byte(prefix + "ghp_" + strings.Repeat("A", 36))
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
			budget := maxGraphBytes
			if publicationInspect(context.Background(), []byte("fixture="+enc.EncodeToString(raw)), 0, &budget) == nil {
				t.Fatal("padded or alphabet variant escaped")
			}
		}
	}
	budget := 1024
	if !errors.Is(publicationInspect(context.Background(), []byte(strings.Repeat("a=", 4096)), 0, &budget), ErrPublicationContent) {
		t.Fatal("byte budget not enforced")
	}
	budget = 128
	if !errors.Is(publicationInspect(context.Background(), []byte(strings.Repeat("z", 100)), 0, &budget), ErrPublicationContent) {
		t.Fatal("failed decode work not charged")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget = maxGraphBytes
	if !errors.Is(publicationInspect(ctx, []byte("fixture=public"), 0, &budget), context.Canceled) {
		t.Fatal("cancellation lost")
	}
	// A long equals chain is scanned once, not by trying each suffix.
	budget = maxGraphBytes
	if e := publicationInspect(context.Background(), []byte(strings.Repeat("a=", 65536)), 0, &budget); e != nil {
		t.Fatal("safe assignment chain denied", e)
	}
}
