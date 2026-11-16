package implementation

import (
	"bytes"
	"sync"
	"testing"
)

func TestLockedDependencyVerificationAuthenticatesBytes(t *testing.T) {
	source, bundle := toolSeedFaultInputs(t)
	wire, err := dependencyBundleBytes(bundle)
	if err != nil {
		t.Fatal(err)
	}
	mod, sum := source.Files[0].Content, source.Files[1].Content
	if err := validateLockedDependencyBundle(wire, mod, sum); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"archive", "mod", "sum", "truncated", "missing"} {
		t.Run(kind, func(t *testing.T) {
			w, m, s := bytes.Clone(wire), bytes.Clone(mod), bytes.Clone(sum)
			switch kind {
			case "archive":
				index := bytes.Index(w, []byte("module github.com/a-h/templ"))
				if index < 0 {
					t.Fatal("fixture archive")
				}
				w[index] = 'M'
			case "mod":
				m = bytes.ReplaceAll(m, []byte("v0.0.1"), []byte("v0.0.2"))
			case "sum":
				s = bytes.ReplaceAll(s, []byte("h1:"), []byte("h1:A"))
			case "truncated":
				w = w[:100]
			case "missing":
				m = append(m, []byte("require example.test/extra v1.0.0\n")...)
			}
			if err := verifyLockedDependencyBundle(w, m, s); err == nil {
				t.Fatal("fault not independently rejected")
			}
			if err := validateLockedDependencyBundle(w, m, s); err == nil {
				t.Fatal("changed bytes reused validation")
			}
			if err := validateLockedDependencyBundle(wire, mod, sum); err != nil {
				t.Fatal("invalid input poisoned verified identity", err)
			}
		})
	}
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := bytes.Clone(wire)
			m := bytes.Clone(mod)
			s := bytes.Clone(sum)
			if err := validateLockedDependencyBundle(w, m, s); err != nil {
				t.Error(err)
			}
			w[0] ^= 1
			if err := validateLockedDependencyBundle(w, m, s); err == nil {
				t.Error("private archive mutation accepted")
			}
		}()
	}
	wg.Wait()
	if err := validateLockedDependencyBundle(wire, mod, sum); err != nil {
		t.Fatal(err)
	}
}
