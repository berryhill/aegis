package manager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPlatformScopedDiscovery(t *testing.T) {
	ctx := context.Background()
	schema := map[string]any{"type": "object", "properties": map[string]any{"revision": map[string]any{"type": "integer"}}, "required": []string{"revision"}, "additionalProperties": false}
	p := &changingPlatformCatalog{caps: []PlatformCapability{{Operation: "loops.show", Mutation: false, Service: "GetLoopViewAs", Availability: "supported", Reason: "exact revision", InputSchema: schema}}}
	s := catalogSession(t, p, &fakeGateway{})
	before, _ := json.Marshal(p.caps)
	input, digest, err := s.platformModelInput(ctx, "Show a loop")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(input, "input_schema") || strings.Contains(input, "additionalProperties") || !strings.Contains(input, `["loops.show",false,"GetLoopViewAs","supported","exact revision"]`) || !strings.Contains(input, digest) {
		t.Fatal("index loses metadata or includes schema")
	}
	args := &PlatformDiscoverArguments{Operation: "loops.show", CatalogDigest: digest}
	result, err := s.executePlatform(ctx, PlatformDiscover, args)
	if err != nil {
		t.Fatal(err)
	}
	exact, _ := json.Marshal(p.caps[0])
	if !strings.Contains(result, string(exact)) || !strings.Contains(result, digest) {
		t.Fatal("schema/metadata incomplete")
	}
	next, _, _ := s.platformModelInput(ctx, "Which fields do you need?")
	if !strings.Contains(next, string(exact)) {
		t.Fatal("schema unavailable for input collection")
	}
	after, _ := json.Marshal(p.caps)
	if string(before) != string(after) || s.platformPending != nil {
		t.Fatal("discovery mutated catalog or prepared effect")
	}
	legacy, err := s.executePlatform(ctx, PlatformDiscover, &PlatformDiscoverArguments{})
	if err != nil || !strings.Contains(legacy, string(before)) {
		t.Fatal("legacy full discovery changed", err)
	}
	for _, name := range []string{"missing", "loops", "LOOPS.SHOW"} {
		result, err = s.executePlatform(ctx, PlatformDiscover, &PlatformDiscoverArguments{Operation: name, CatalogDigest: digest})
		if err != nil || !strings.Contains(result, "platform_operation_unknown") {
			t.Fatal("inexact selector accepted", name, result, err)
		}
	}
	p.caps = append(p.caps, p.caps[0])
	b, _ := json.Marshal(p.caps)
	result, err = s.executePlatform(ctx, PlatformDiscover, &PlatformDiscoverArguments{Operation: "loops.show", CatalogDigest: digestString(string(b))})
	if err != nil || !strings.Contains(result, "platform_operation_ambiguous") {
		t.Fatal("ambiguous operation accepted")
	}
	p.caps = p.caps[:1]
	p.caps[0].InputSchema = map[string]any{"changed": true}
	result, err = s.executePlatform(ctx, PlatformDiscover, args)
	if err != nil || !strings.Contains(result, "platform_catalog_changed") {
		t.Fatal("stale schema accepted")
	}
	refreshed, newDigest, _ := s.platformModelInput(ctx, "again")
	if newDigest == digest || !strings.Contains(refreshed, newDigest) {
		t.Fatal("schema-only change not bound")
	}
	p.caps[0].InputSchema = map[string]any{"description": strings.Repeat("x", 25<<10)}
	b, _ = json.Marshal(p.caps)
	result, err = s.executePlatform(ctx, PlatformDiscover, &PlatformDiscoverArguments{Operation: "loops.show", CatalogDigest: digestString(string(b))})
	if err != nil || !strings.Contains(result, "platform_schema_exceeds_context_bound") || strings.Contains(result, strings.Repeat("x", 100)) {
		t.Fatal("oversized schema silently truncated")
	}
	p.err = context.Canceled
	if _, err = s.executePlatform(ctx, PlatformDiscover, args); err == nil {
		t.Fatal("discovery skipped fresh admission")
	}
}

func TestPlatformDiscoveryClosedArguments(t *testing.T) {
	for _, raw := range []string{`null`, `{"operation":"loops.show"}`, `{"catalog_digest":"x"}`, `{"operation":null,"catalog_digest":"x"}`, `{"operation":"","catalog_digest":"x"}`, `{"operation":"loops.show","catalog_digest":"x","principal":"owner"}`} {
		if _, err := decodeArguments(Proposal{Operation: PlatformDiscover, Arguments: json.RawMessage(raw)}); err == nil {
			t.Fatal("accepted invalid discovery arguments", raw)
		}
	}
	for _, raw := range []string{`{}`, `{"operation":"loops.show","catalog_digest":"sha256:abc"}`} {
		if _, err := decodeArguments(Proposal{Operation: PlatformDiscover, Arguments: json.RawMessage(raw)}); err != nil {
			t.Fatal(err)
		}
	}
}
