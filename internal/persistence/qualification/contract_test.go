package qualification

import (
	"testing"
	"time"
)

func TestEveryMatrixEntryIsQualifiedAndPlanesAreSeparate(t *testing.T) {
	matrix := Matrix()
	if len(matrix) != 4 {
		t.Fatalf("qualification matrix has %d entries, want 4", len(matrix))
	}
	if matrix[0].Plane != PlaneSessionAuthority || matrix[0].Backend != BackendBadger {
		t.Fatalf("session authority qualification changed: %+v", matrix[0])
	}
	if matrix[1].Plane != PlaneCredentials || matrix[1].Backend != BackendBBolt {
		t.Fatalf("credential qualification changed: %+v", matrix[1])
	}
	if matrix[2].Plane != PlaneFleetDefinitions || matrix[2].Backend != BackendBadger || matrix[3].Plane != PlaneFleetLifecycle || matrix[3].Backend != BackendBadger {
		t.Fatalf("fleet qualification changed: definitions=%+v lifecycle=%+v", matrix[2], matrix[3])
	}
	for _, contract := range matrix {
		if err := Validate(contract); err != nil {
			t.Fatalf("qualified baseline rejected: %v", err)
		}
	}
}

func TestFleetPlanesShareOneExactQualifiedAtomicStore(t *testing.T) {
	definitions, err := Baseline(PlaneFleetDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := Baseline(PlaneFleetLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	if definitions.RootRelativePath != FleetRootRelativePath || lifecycle.RootRelativePath != FleetRootRelativePath || definitions.SchemaVersion != FleetSchemaVersion || lifecycle.SchemaVersion != FleetSchemaVersion {
		t.Fatalf("fleet planes do not share exact store: definitions=%+v lifecycle=%+v", definitions, lifecycle)
	}
	root, err := FleetRoot("/srv/aegis/state")
	if err != nil || root != "/srv/aegis/state/persistence/fleet-v1" {
		t.Fatalf("root=%q err=%v", root, err)
	}
	for _, invalidStateDir := range []string{"", "state", "/", "/srv/aegis/../state"} {
		if root, err = FleetRoot(invalidStateDir); err == nil {
			t.Fatalf("invalid state directory accepted: state=%q root=%q", invalidStateDir, root)
		}
	}
	for _, substituted := range []string{"fleet-v1", "/srv/aegis/state/persistence/definitions-v1", "/srv/aegis/state/persistence/fleet-v1/../queue-v1"} {
		if err = ValidateFleetRoot("/srv/aegis/state", substituted); err == nil {
			t.Fatalf("substituted root accepted: %q", substituted)
		}
	}
}

func TestFleetReadinessFailsClosed(t *testing.T) {
	contract, err := Baseline(PlaneFleetLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	valid := FleetReadinessEvidence{
		Contract: contract, StateDir: "/srv/aegis/state", Root: "/srv/aegis/state/persistence/fleet-v1",
		DirectoryMode: 0700, FileMode: 0600, WriterLockHeld: true,
		AvailableBytes: FleetDiskReserveBytes, SchemaVersion: FleetSchemaVersion,
		MigrationComplete: true, LastShutdownClean: true,
	}
	if err = ValidateFleetReadiness(valid); err != nil {
		t.Fatalf("qualified readiness rejected: %v", err)
	}
	tests := []struct {
		name string
		edit func(*FleetReadinessEvidence)
	}{
		{"wrong plane", func(e *FleetReadinessEvidence) { e.Contract = qualified[PlaneSessionAuthority] }},
		{"path", func(e *FleetReadinessEvidence) { e.Root += "-other" }},
		{"directory mode", func(e *FleetReadinessEvidence) { e.DirectoryMode = 0750 }},
		{"file mode", func(e *FleetReadinessEvidence) { e.FileMode = 0640 }},
		{"writer lock", func(e *FleetReadinessEvidence) { e.WriterLockHeld = false }},
		{"reserve", func(e *FleetReadinessEvidence) { e.AvailableBytes-- }},
		{"schema", func(e *FleetReadinessEvidence) { e.SchemaVersion = "fleet-v2" }},
		{"migration", func(e *FleetReadinessEvidence) { e.MigrationComplete = false }},
		{"dirty", func(e *FleetReadinessEvidence) { e.LastShutdownClean = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := valid
			test.edit(&evidence)
			if err := ValidateFleetReadiness(evidence); err == nil {
				t.Fatal("incomplete or substituted readiness accepted")
			}
		})
	}
	dirtyRecovered := valid
	dirtyRecovered.LastShutdownClean = false
	dirtyRecovered.DirtyRecoveryVerified = true
	if err = ValidateFleetReadiness(dirtyRecovered); err != nil {
		t.Fatalf("verified dirty recovery rejected: %v", err)
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
		{"fleet schema", PlaneFleetDefinitions, func(c *Contract) { c.SchemaVersion = "fleet-v2" }},
		{"fleet root", PlaneFleetDefinitions, func(c *Contract) { c.RootRelativePath = "persistence/definitions-v1" }},
		{"fleet lock", PlaneFleetLifecycle, func(c *Contract) { c.LockModel = "best-effort" }},
		{"fleet reserve", PlaneFleetLifecycle, func(c *Contract) { c.DiskReserveBytes-- }},
		{"fleet clean shutdown", PlaneFleetLifecycle, func(c *Contract) { c.CleanShutdownRequired = false }},
		{"fleet dirty open", PlaneFleetLifecycle, func(c *Contract) { c.DirtyOpenPolicy = "ignore" }},
		{"fleet migration", PlaneFleetDefinitions, func(c *Contract) { c.MigrationPolicy = "in-place" }},
		{"fleet backup", PlaneFleetDefinitions, func(c *Contract) { c.BackupPolicy = "copy-files" }},
		{"fleet readiness", PlaneFleetLifecycle, func(c *Contract) { c.ReadinessPolicy = "process-up" }},
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
