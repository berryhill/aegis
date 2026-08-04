package qualification

import (
	"testing"
	"time"
)

func TestBaselineIsQualified(t *testing.T) {
	if err := Validate(Baseline()); err != nil {
		t.Fatalf("qualified baseline rejected: %v", err)
	}
}

func TestValidateRejectsEveryUnqualifiedContractDimension(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Contract)
	}{
		{"backend", func(c *Contract) { c.Backend = "unknown" }},
		{"module path", func(c *Contract) { c.ModulePath = "example.invalid/bbolt" }},
		{"module version", func(c *Contract) { c.ModuleVersion = "v1.5.1" }},
		{"operating system", func(c *Contract) { c.GOOS = "darwin" }},
		{"architecture", func(c *Contract) { c.GOARCH = "arm64" }},
		{"filesystem", func(c *Contract) { c.Filesystem = "xfs" }},
		{"authority", func(c *Contract) { c.Authority = "multi-process" }},
		{"directory mode", func(c *Contract) { c.DirectoryMode = 0750 }},
		{"file mode", func(c *Contract) { c.FileMode = 0640 }},
		{"lock timeout", func(c *Contract) { c.LockTimeout = time.Second }},
		{"multiple writers", func(c *Contract) { c.SingleWriter = false }},
		{"no sync", func(c *Contract) { c.NoSync = true }},
		{"no grow sync", func(c *Contract) { c.NoGrowSync = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := Baseline()
			test.edit(&contract)
			if err := Validate(contract); err == nil {
				t.Fatal("unqualified persistence contract accepted")
			}
		})
	}
}
