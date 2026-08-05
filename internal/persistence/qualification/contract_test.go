package qualification

import (
	"testing"
	"time"
)

func TestEveryMatrixEntryIsQualifiedAndPlanesAreSeparate(t *testing.T) {
	matrix := Matrix()
	if len(matrix) != 2 {
		t.Fatalf("qualification matrix has %d entries, want 2", len(matrix))
	}
	if matrix[0].Plane != PlaneSessionAuthority || matrix[0].Backend != BackendBadger {
		t.Fatalf("session authority qualification changed: %+v", matrix[0])
	}
	if matrix[1].Plane != PlaneCredentials || matrix[1].Backend != BackendBBolt {
		t.Fatalf("credential qualification changed: %+v", matrix[1])
	}
	for _, contract := range matrix {
		if err := Validate(contract); err != nil {
			t.Fatalf("qualified baseline rejected: %v", err)
		}
	}
}

func TestUnknownPlaneDenies(t *testing.T) {
	if _, err := Baseline("audit"); err == nil {
		t.Fatal("unknown persistence plane accepted")
	}
	if err := Validate(Contract{Plane: "audit"}); err == nil {
		t.Fatal("unknown persistence contract accepted")
	}
}

func TestValidateRejectsEveryUnqualifiedContractDimension(t *testing.T) {
	tests := []struct {
		name  string
		plane Plane
		edit  func(*Contract)
	}{
		{"status", PlaneSessionAuthority, func(c *Contract) { c.Status = "provisional" }},
		{"backend", PlaneSessionAuthority, func(c *Contract) { c.Backend = "unknown" }},
		{"module path", PlaneSessionAuthority, func(c *Contract) { c.ModulePath = "example.invalid/badger" }},
		{"module version", PlaneSessionAuthority, func(c *Contract) { c.ModuleVersion = "v4.9.6" }},
		{"operating system", PlaneSessionAuthority, func(c *Contract) { c.GOOS = "darwin" }},
		{"architecture", PlaneSessionAuthority, func(c *Contract) { c.GOARCH = "arm64" }},
		{"filesystem", PlaneSessionAuthority, func(c *Contract) { c.Filesystem = "xfs" }},
		{"authority", PlaneSessionAuthority, func(c *Contract) { c.Authority = "multi-process" }},
		{"directory mode", PlaneSessionAuthority, func(c *Contract) { c.DirectoryMode = 0750 }},
		{"file mode", PlaneSessionAuthority, func(c *Contract) { c.FileMode = 0640 }},
		{"badger sync writes", PlaneSessionAuthority, func(c *Contract) { c.SyncWrites = false }},
		{"badger conflicts", PlaneSessionAuthority, func(c *Contract) { c.DetectConflicts = false }},
		{"badger bbolt option", PlaneSessionAuthority, func(c *Contract) { c.LockTimeout = time.Second }},
		{"bbolt lock timeout", PlaneCredentials, func(c *Contract) { c.LockTimeout = time.Second }},
		{"bbolt no sync", PlaneCredentials, func(c *Contract) { c.NoSync = true }},
		{"bbolt no grow sync", PlaneCredentials, func(c *Contract) { c.NoGrowSync = true }},
		{"bbolt badger option", PlaneCredentials, func(c *Contract) { c.SyncWrites = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract, err := Baseline(test.plane)
			if err != nil {
				t.Fatal(err)
			}
			test.edit(&contract)
			if err := Validate(contract); err == nil {
				t.Fatal("unqualified persistence contract accepted")
			}
		})
	}
}
