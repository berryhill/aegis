package api

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/berryhill/aegis/internal/app"
	"github.com/berryhill/aegis/internal/loop"
)

func TestConsoleLoopBookmarkSelectorBoundsAndLiteralReferences(t *testing.T) {
	maximumID := strings.Repeat("a", 255)
	maximumRevision := strconv.FormatUint(^uint64(0), 10)
	for _, key := range []string{"logical-loop", "team/loop:stage", maximumID} {
		needed, err := loopBookmarkNeedsResolution(key)
		if err != nil || !needed {
			t.Fatalf("valid logical selector %q: needed=%v err=%v", key, needed, err)
		}
	}
	for _, key := range []string{"logical-loop:2", "team/loop:stage:3", maximumID + ":" + maximumRevision} {
		needed, err := loopBookmarkNeedsResolution(key)
		if err != nil || needed {
			t.Fatalf("exact immutable key changed: needed=%v err=%v", needed, err)
		}
	}
	for _, key := range []string{" logical-loop", "/external", "logical-loop:0", "logical-loop:01", "logical-loop:" + strings.Repeat("9", 20), strings.Repeat("a", 256), "logical-loop\n", "logical-loop?stanza=owner"} {
		if _, err := loopBookmarkNeedsResolution(key); err == nil {
			t.Fatalf("invalid read key accepted without format validation: %q", key)
		}
	}
}

func TestConsoleLoopBookmarkPinsServerHeadAndOnlyReadFilters(t *testing.T) {
	inventory := []app.LoopView{{Revision: loop.LoopRevision{LoopID: "logical-loop", Revision: 1, Digest: "first"}}, {Revision: loop.LoopRevision{LoopID: "logical-loop", Revision: 2, Digest: "second"}}}
	query := url.Values{"q": {"review"}, "lifecycle": {"all"}, "availability": {"archived"}, "page": {"2"}, "limit": {"20"}, "revision": {"99"}, "stanza": {"principal"}, "subject": {"spoof"}, "origin": {"https://external.invalid"}}
	target, err := loopBookmarkTarget("logical-loop", inventory, query)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.IsAbs() || parsed.Host != "" || parsed.Path != "/console/loops" || parsed.Query().Get("record_key") != "logical-loop:2" || parsed.Fragment != "/loops/logical-loop:2" {
		t.Fatal("bookmark was not pinned to exact same-origin authoritative head")
	}
	for _, key := range []string{"q", "lifecycle", "availability", "page", "limit"} {
		if parsed.Query().Get(key) != query.Get(key) {
			t.Fatalf("lost presentation filter %s", key)
		}
	}
	for _, key := range []string{"revision", "stanza", "subject", "origin"} {
		if parsed.Query().Has(key) {
			t.Fatalf("unsupported selector propagated: %s", key)
		}
	}
	if query.Get("revision") != "99" {
		t.Fatal("read resolution mutated caller input")
	}
	if _, err := loopBookmarkTarget("missing-loop", inventory, query); err == nil {
		t.Fatal("missing logical Loop fabricated")
	}
	query["q"] = []string{"one", "two"}
	if _, err := loopBookmarkTarget("logical-loop", inventory, query); err == nil {
		t.Fatal("ambiguous filters accepted")
	}
}
