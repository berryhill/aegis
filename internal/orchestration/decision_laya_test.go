package orchestration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeLayaProcess func(context.Context, []byte) ([]byte, error)

func (f fakeLayaProcess) Run(ctx context.Context, in []byte) ([]byte, error) { return f(ctx, in) }

func TestLayaTypedGateAndVerdict(t *testing.T) {
	p := fakeLayaProcess(func(_ context.Context, in []byte) ([]byte, error) {
		if !strings.Contains(string(in), `"kind":"gate"`) {
			t.Fatalf("request=%s", in)
		}
		return []byte(`{"version":1,"kind":"gate","answers":{"specified":{"choice":"yes","answer_confidence":0.6},"result_defined":{"choice":"no","answer_confidence":0.95}}}`), nil
	})
	d := NewLayaDecisionAdapter(p)
	got, err := d.Gate(context.Background(), "one bounded task")
	if err != nil || !got.Specified || got.ResultDefined {
		t.Fatalf("gate=%+v err=%v", got, err)
	}
	p = fakeLayaProcess(func(_ context.Context, in []byte) ([]byte, error) {
		if !strings.Contains(string(in), `"kind":"verdict"`) {
			t.Fatalf("request=%s", in)
		}
		return []byte(`{"version":1,"kind":"verdict","answers":{"done":{"choice":"yes","answer_confidence":0.9},"stays_in_scope":{"choice":"no","answer_confidence":0.9},"fulfills":{"choice":"yes","answer_confidence":0.9},"works":{"choice":"yes","answer_confidence":0.9},"practices":{"choice":"yes","answer_confidence":0.9}}}`), nil
	})
	d = NewLayaDecisionAdapter(p)
	verdict, err := d.Verdict(context.Background(), "task", "report")
	if err != nil || !verdict.Done || verdict.StaysInScope || !verdict.Fulfills || !verdict.Works || !verdict.Practices {
		t.Fatalf("verdict=%+v err=%v", verdict, err)
	}
}

func TestLayaFailClosed(t *testing.T) {
	good := `{"version":1,"kind":"gate","answers":{"specified":{"choice":"yes","answer_confidence":0.6},"result_defined":{"choice":"yes","answer_confidence":0.9}}}`
	cases := map[string]string{
		"empty": "", "truncated": `{"version":1`, "extra": good + `{}`, "duplicate": strings.Replace(good, `"version":1`, `"version":1,"version":1`, 1),
		"unknown":             strings.Replace(good, `"version":1`, `"version":1,"authority":"grant"`, 1),
		"wrong_kind":          strings.Replace(good, `"kind":"gate"`, `"kind":"verdict"`, 1),
		"missing":             strings.Replace(good, `,"result_defined":{"choice":"yes","answer_confidence":0.9}`, "", 1),
		"unknown_label":       strings.Replace(good, `"result_defined"`, `"other"`, 1),
		"low":                 strings.Replace(good, `"answer_confidence":0.6`, `"answer_confidence":0.59`, 1),
		"ambiguous":           strings.Replace(good, `"choice":"yes"`, `"choice":"maybe"`, 1),
		"null":                strings.Replace(good, `"choice":"yes"`, `"choice":null`, 1),
		"nan":                 strings.Replace(good, `"answer_confidence":0.6`, `"answer_confidence":NaN`, 1),
		"out_of_range":        strings.Replace(good, `"answer_confidence":0.6`, `"answer_confidence":1.1`, 1),
		"extra_answer":        strings.Replace(good, `"choice":"yes"`, `"choice":"yes","reason":"trust me"`, 1),
		"case_folded_version": strings.Replace(good, `"version":1`, `"version":1,"Version":1`, 1),
		"case_folded_choice":  strings.Replace(good, `"choice":"yes"`, `"choice":"yes","Choice":"yes"`, 1),
	}
	for name, response := range cases {
		t.Run(name, func(t *testing.T) {
			d := NewLayaDecisionAdapter(fakeLayaProcess(func(context.Context, []byte) ([]byte, error) { return []byte(response), nil }))
			got, err := d.Gate(context.Background(), "task")
			if err == nil || got.Specified || got.ResultDefined {
				t.Fatalf("unexpected gate=%+v err=%v", got, err)
			}
		})
	}
	t.Run("unavailable", func(t *testing.T) {
		d := NewLayaDecisionAdapter(fakeLayaProcess(func(context.Context, []byte) ([]byte, error) { return []byte(good), errors.New("unavailable") }))
		if got, err := d.Gate(context.Background(), "task"); err == nil || got.Specified {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		d := NewLayaDecisionAdapter(fakeLayaProcess(func(ctx context.Context, _ []byte) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() }))
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		if got, err := d.Gate(ctx, "task"); err == nil || got.Specified {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("nil_process", func(t *testing.T) {
		if got, err := NewLayaDecisionAdapter(nil).Gate(context.Background(), "task"); err == nil || got.Specified {
			t.Fatalf("%+v %v", got, err)
		}
	})
}

func TestLayaInputBounds(t *testing.T) {
	d := NewLayaDecisionAdapter(fakeLayaProcess(func(context.Context, []byte) ([]byte, error) { t.Fatal("process called"); return nil, nil }))
	if _, err := d.Gate(context.Background(), strings.Repeat("x", 65537)); err == nil {
		t.Fatal("oversized state accepted")
	}
	if _, err := d.Gate(context.Background(), strings.Repeat("\\", 40000)); err == nil {
		t.Fatal("oversized encoded request accepted")
	}
	if _, err := d.Verdict(context.Background(), "", "report"); err == nil {
		t.Fatal("empty request accepted")
	}
}

func TestLocalLayaProcessRejectsUnconfiguredExecutable(t *testing.T) {
	for _, binary := range []string{"", "python3"} {
		p := LocalLayaProcess{PythonExecutable: binary}
		if _, err := p.Run(context.Background(), []byte(`{"version":1}`)); err == nil {
			t.Fatalf("accepted executable %q", binary)
		}
	}
	d := NewLayaDecisionAdapter(LocalLayaProcess{PythonExecutable: "/nonexistent/aegis-laya-python"})
	if got, err := d.Gate(context.Background(), "task"); err == nil || got.Specified {
		t.Fatalf("unavailable interpreter granted gate: %+v %v", got, err)
	}
}

func TestLocalLayaProcessDoesNotInheritControllerSecrets(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0700); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(t.TempDir(), "probe")
	script := "#!/bin/sh\n" +
		"test -z \"$AEGIS_LAYA_SECRET_PROBE\" || exit 1\n" +
		"test \"$HOME\" = '" + home + "' || exit 1\n" +
		"printf '%s' '{\"version\":1,\"kind\":\"gate\",\"answers\":{\"specified\":{\"choice\":\"yes\",\"answer_confidence\":0.9},\"result_defined\":{\"choice\":\"yes\",\"answer_confidence\":0.9}}}'\n"
	if err := os.WriteFile(probe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AEGIS_LAYA_SECRET_PROBE", "must-not-reach-child")
	d := NewLayaDecisionAdapter(LocalLayaProcess{PythonExecutable: probe, Home: home})
	got, err := d.Gate(context.Background(), "task")
	if err != nil || !got.Specified || !got.ResultDefined {
		t.Fatalf("isolated Laya process: %+v %v", got, err)
	}
	if _, err := NewLayaDecisionAdapter(LocalLayaProcess{PythonExecutable: probe, Home: t.TempDir() + "/missing"}).Gate(context.Background(), "task"); err == nil {
		t.Fatal("nonexistent Laya home accepted")
	}
}
