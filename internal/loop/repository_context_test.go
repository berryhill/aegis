package loop

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRepositoryContextManifest(t *testing.T) {
	valid := func() RepositoryContext {
		return RepositoryContext{TaskDigest: sha256Digest([]byte("task")), SourceDigest: sha256Digest([]byte("source")), MaxFiles: 2, MaxFileBytes: 20, MaxTotalBytes: 30, Files: []RepositoryContextFile{{Path: "main.go", Bytes: 10, Digest: sha256Digest([]byte("source")), Role: "source"}, {Path: "main_test.go", Bytes: 10, Digest: sha256Digest([]byte("test")), Role: "trusted_test"}}}
	}
	for _, tc := range []struct {
		name     string
		mutate   func(*RepositoryContext)
		writable []string
		deny     bool
	}{
		{"source preimage writable", func(*RepositoryContext) {}, []string{"main.go"}, false},
		{"test writable", func(*RepositoryContext) {}, []string{"main_test.go"}, true},
		{"task binding", func(m *RepositoryContext) { m.TaskDigest = sha256Digest([]byte("other")) }, nil, true},
		{"source binding", func(m *RepositoryContext) { m.SourceDigest = "main" }, nil, true},
		{"file count", func(m *RepositoryContext) { m.MaxFiles = 1 }, nil, true},
		{"count upper bound", func(m *RepositoryContext) { m.MaxFiles = 129 }, nil, true},
		{"per file", func(m *RepositoryContext) { m.MaxFileBytes = 9 }, nil, true},
		{"aggregate", func(m *RepositoryContext) { m.MaxFileBytes = 15; m.MaxTotalBytes = 19 }, nil, true},
		{"byte upper bound", func(m *RepositoryContext) { m.MaxTotalBytes = MaxRepositoryContextBytes + 1 }, nil, true},
		{"duplicate", func(m *RepositoryContext) { m.Files[1] = m.Files[0] }, nil, true},
		{"role confusion", func(m *RepositoryContext) { m.Files[1].Role = "source" }, nil, true},
		{"unknown role", func(m *RepositoryContext) { m.Files[0].Role = "authority" }, nil, true},
		{"bad digest", func(m *RepositoryContext) { m.Files[0].Digest = "bad" }, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := valid()
			tc.mutate(&m)
			err := m.Validate("task", tc.writable)
			if (err != nil) != tc.deny {
				t.Fatal("manifest validation mismatch")
			}
		})
	}
	m := valid()
	a, _ := m.Digest()
	m.SourceDigest = sha256Digest([]byte("changed"))
	b, _ := m.Digest()
	if a == b {
		t.Fatal("identity did not bind provenance")
	}
}

func TestRepositoryContextLegacyCanonicalIdentity(t *testing.T) {
	const legacy = `{"schema_version":"aegis.loop.verified-implementation.v1","task":"task","acceptance":"tests pass","workspace":"/workspace","writable_files":["main.go"],"policy":{"required_tests":[{"package":"example","name":"TestMain"}],"kind":"go-test.v1","packages":["."],"timeout_seconds":60},"max_passes":2}`
	c, err := DecodeVerifiedImplementation([]byte(legacy))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	d, err := c.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != legacy || strings.Contains(string(wire), "repository_context") || d != sha256Digest([]byte(legacy)) {
		t.Fatal("legacy canonical bytes changed")
	}
}
