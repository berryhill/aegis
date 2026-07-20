package manager

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
)

func TestMessagePreviewStreamsCanonicalMessageSnapshots(t *testing.T) {
	preview := newMessagePreview(4096)
	var snapshots []string
	emit := func(value string) error {
		snapshots = append(snapshots, value)
		return nil
	}
	chunks := []string{
		`{"schema_version":"aegis.manager.response.v1","kind":"message","message":"Hello `,
		`world\nwith \u2603`,
		`","proposal":null}`,
	}
	for _, chunk := range chunks {
		if err := preview.Feed([]byte(chunk), emit); err != nil {
			t.Fatal(err)
		}
	}
	if err := preview.Complete("Hello world\nwith ☃", emit); err != nil {
		t.Fatal(err)
	}
	if !preview.Released() || len(snapshots) < 2 || snapshots[len(snapshots)-1] != "Hello world\nwith ☃" {
		t.Fatalf("snapshots=%q", snapshots)
	}
	for index := 1; index < len(snapshots); index++ {
		if len(snapshots[index]) < len(snapshots[index-1]) || snapshots[index][:len(snapshots[index-1])] != snapshots[index-1] {
			t.Fatalf("non-monotonic snapshots=%q", snapshots)
		}
	}
}

func TestMessagePreviewNeverReleasesProposalOrNonCanonicalEnvelope(t *testing.T) {
	inputs := []string{
		`{"schema_version":"aegis.manager.response.v1","kind":"proposal","message":"do not stream","proposal":{"operation":"status.show","arguments":{}}}`,
		`{"kind":"message","schema_version":"aegis.manager.response.v1","message":"wrong order","proposal":null}`,
	}
	for _, input := range inputs {
		preview := newMessagePreview(4096)
		called := false
		if err := preview.Feed([]byte(input), func(string) error { called = true; return nil }); err != nil {
			t.Fatal(err)
		}
		if called || preview.Released() {
			t.Fatalf("unsafe preview released for %s", input)
		}
	}
}

func TestMessagePreviewWaitsForFragmentedUTF8(t *testing.T) {
	preview := newMessagePreview(4096)
	input := []byte(`{"schema_version":"aegis.manager.response.v1","kind":"message","message":"a sufficiently long snowman: ☃","proposal":null}`)
	marker := bytes.Index(input, []byte("☃"))
	if marker < 0 {
		t.Fatal("fixture marker absent")
	}
	var snapshots []string
	emit := func(value string) error { snapshots = append(snapshots, value); return nil }
	for _, chunk := range [][]byte{input[:marker+1], input[marker+1 : marker+2], input[marker+2:]} {
		if err := preview.Feed(chunk, emit); err != nil {
			t.Fatal(err)
		}
	}
	if err := preview.Complete("a sufficiently long snowman: ☃", emit); err != nil {
		t.Fatal(err)
	}
	if snapshots[len(snapshots)-1] != "a sufficiently long snowman: ☃" {
		t.Fatalf("snapshots=%q", snapshots)
	}
}

func TestMessagePreviewPropagatesRendererFailure(t *testing.T) {
	preview := newMessagePreview(4096)
	want := errors.New("renderer failed")
	input := `{"schema_version":"aegis.manager.response.v1","kind":"message","message":"enough text to cross the retained tail","proposal":null}`
	if err := preview.Feed([]byte(input), func(string) error { return want }); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
}

func TestMessagePreviewRejectsCompletedMessageMismatch(t *testing.T) {
	preview := newMessagePreview(4096)
	input := `{"schema_version":"aegis.manager.response.v1","kind":"message","message":"a long streamed message","proposal":null}`
	if err := preview.Feed([]byte(input), func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := preview.Complete("different completed message", func(string) error { return nil }); err == nil {
		t.Fatal("mismatched completed message accepted")
	}
}

func TestStreamedJSONStringEscapedQuote(t *testing.T) {
	raw, complete := streamedJSONString([]byte(`one \"quoted\" value","proposal":null}`))
	if !complete || !reflect.DeepEqual(raw, []byte(`one \"quoted\" value`)) {
		t.Fatalf("raw=%q complete=%v", raw, complete)
	}
}
