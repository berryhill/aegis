// Package qualification freezes the reviewed persistence combinations for the
// MVI. It is a declarative qualification record, not host discovery: unknown
// planes, engines, versions, platforms, filesystems, and durability modes deny.
package qualification

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

type Plane string

type Status string

const (
	PlaneSessionAuthority Plane = "session-authority"
	PlaneCredentials      Plane = "credential-custody"
	PlaneFleetDefinitions Plane = "fleet-control-definitions"
	PlaneFleetLifecycle   Plane = "fleet-control-lifecycle"

	StatusQualified Status = "qualified"

	BackendBadger = "badger"
	BackendBBolt  = "bbolt"

	BadgerModulePath    = "github.com/dgraph-io/badger/v4"
	BadgerModuleVersion = "v4.9.5"
	BBoltModulePath     = "go.etcd.io/bbolt"
	BBoltModuleVersion  = "v1.5.0"

	QualifiedGOOS       = "linux"
	QualifiedGOARCH     = "amd64"
	QualifiedFilesystem = "ext4"
	AuthorityModel      = "single-aegis-process"

	FleetSchemaVersion    = "fleet-v1"
	FleetRootRelativePath = "persistence/fleet-v1"
	FleetLockModel        = "exclusive-aegis-process-lock"
	FleetDirtyOpenPolicy  = "verify-integrity-and-replay-before-readiness"
	FleetMigrationPolicy  = "offline-copy-verify-no-replace"
	FleetBackupPolicy     = "consistent-snapshot-verify-before-restore"
	FleetReadinessPolicy  = "exact-schema-clean-or-recovered-single-writer"
	FleetDiskReserveBytes = uint64(256 * 1024 * 1024)
)

// Contract is one complete reviewed storage combination. Engine-specific
// switches remain separate so that a setting meaningful to one engine cannot
// be mistaken for a generic durability promise.
type Contract struct {
	Plane                 Plane
	Status                Status
	Backend               string
	ModulePath            string
	ModuleVersion         string
	GOOS                  string
	GOARCH                string
	Filesystem            string
	Authority             string
	DirectoryMode         uint32
	FileMode              uint32
	SchemaVersion         string
	RootRelativePath      string
	LockModel             string
	DiskReserveBytes      uint64
	CleanShutdownRequired bool
	DirtyOpenPolicy       string
	MigrationPolicy       string
	BackupPolicy          string
	ReadinessPolicy       string

	// Badger-only controls.
	SyncWrites      bool
	DetectConflicts bool

	// bbolt-only controls.
	LockTimeout time.Duration
	NoSync      bool
	NoGrowSync  bool
}

var qualified = map[Plane]Contract{
	PlaneSessionAuthority: {
		Plane:           PlaneSessionAuthority,
		Status:          StatusQualified,
		Backend:         BackendBadger,
		ModulePath:      BadgerModulePath,
		ModuleVersion:   BadgerModuleVersion,
		GOOS:            QualifiedGOOS,
		GOARCH:          QualifiedGOARCH,
		Filesystem:      QualifiedFilesystem,
		Authority:       AuthorityModel,
		DirectoryMode:   0700,
		FileMode:        0600,
		SyncWrites:      true,
		DetectConflicts: true,
	},
	PlaneCredentials: {
		Plane:         PlaneCredentials,
		Status:        StatusQualified,
		Backend:       BackendBBolt,
		ModulePath:    BBoltModulePath,
		ModuleVersion: BBoltModuleVersion,
		GOOS:          QualifiedGOOS,
		GOARCH:        QualifiedGOARCH,
		Filesystem:    QualifiedFilesystem,
		Authority:     AuthorityModel,
		DirectoryMode: 0700,
		FileMode:      0600,
		LockTimeout:   2 * time.Second,
	},
	PlaneFleetDefinitions: fleetContract(PlaneFleetDefinitions),
	PlaneFleetLifecycle:   fleetContract(PlaneFleetLifecycle),
}

func fleetContract(plane Plane) Contract {
	return Contract{
		Plane: plane, Status: StatusQualified, Backend: BackendBadger,
		ModulePath: BadgerModulePath, ModuleVersion: BadgerModuleVersion,
		GOOS: QualifiedGOOS, GOARCH: QualifiedGOARCH, Filesystem: QualifiedFilesystem,
		Authority: AuthorityModel, DirectoryMode: 0700, FileMode: 0600,
		SchemaVersion: FleetSchemaVersion, RootRelativePath: FleetRootRelativePath,
		LockModel: FleetLockModel, DiskReserveBytes: FleetDiskReserveBytes,
		CleanShutdownRequired: true, DirtyOpenPolicy: FleetDirtyOpenPolicy,
		MigrationPolicy: FleetMigrationPolicy, BackupPolicy: FleetBackupPolicy,
		ReadinessPolicy: FleetReadinessPolicy,
		SyncWrites:      true, DetectConflicts: true,
	}
}

// Baseline returns a copy of the only qualified contract for plane.
func Baseline(plane Plane) (Contract, error) {
	contract, ok := qualified[plane]
	if !ok {
		return Contract{}, fmt.Errorf("persistence plane %q is not qualified", plane)
	}
	return contract, nil
}

// Matrix returns the complete MVI qualification matrix in stable plane order.
func Matrix() []Contract {
	return []Contract{
		qualified[PlaneSessionAuthority],
		qualified[PlaneCredentials],
		qualified[PlaneFleetDefinitions],
		qualified[PlaneFleetLifecycle],
	}
}

// Validate fails closed unless input exactly matches its reviewed baseline.
func Validate(contract Contract) error {
	baseline, err := Baseline(contract.Plane)
	if err != nil {
		return err
	}
	if contract != baseline {
		return errors.New("persistence engine, version, platform, filesystem, authority, lifecycle, or durability combination is not qualified")
	}
	return nil
}

// FleetRoot derives the only qualified fleet store root. The definitions and
// lifecycle planes deliberately share one engine so submission, immutable run
// snapshot publication, durable rejection, and queue insertion can be one
// transaction. Callers cannot redirect either plane to an alternate path.
func FleetRoot(stateDir string) (string, error) {
	if stateDir == "" || !filepath.IsAbs(stateDir) || filepath.Clean(stateDir) != stateDir || stateDir == string(filepath.Separator) {
		return "", errors.New("state directory must be a clean absolute non-root path")
	}
	return filepath.Join(stateDir, FleetRootRelativePath), nil
}

// ValidateFleetRoot rejects path substitution, including use of a separately
// named definitions or queue database that would break the atomic boundary.
func ValidateFleetRoot(stateDir, root string) error {
	expected, err := FleetRoot(stateDir)
	if err != nil {
		return err
	}
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || root != expected {
		return errors.New("fleet persistence root is not the qualified state/persistence/fleet-v1 path")
	}
	return nil
}

// FleetReadinessEvidence contains implementation-observed, non-model evidence
// required before either fleet plane may serve an operation.
type FleetReadinessEvidence struct {
	Contract              Contract
	StateDir              string
	Root                  string
	DirectoryMode         uint32
	FileMode              uint32
	WriterLockHeld        bool
	AvailableBytes        uint64
	SchemaVersion         string
	MigrationComplete     bool
	LastShutdownClean     bool
	DirtyRecoveryVerified bool
}

// ValidateFleetReadiness is intentionally strict: unknown or partial evidence,
// an unclean open without verified recovery, and any changed qualified
// dimension all deny readiness.
func ValidateFleetReadiness(evidence FleetReadinessEvidence) error {
	if evidence.Contract.Plane != PlaneFleetDefinitions && evidence.Contract.Plane != PlaneFleetLifecycle {
		return errors.New("fleet readiness requires a qualified fleet plane")
	}
	if err := Validate(evidence.Contract); err != nil {
		return err
	}
	if err := ValidateFleetRoot(evidence.StateDir, evidence.Root); err != nil {
		return err
	}
	if evidence.DirectoryMode != evidence.Contract.DirectoryMode || evidence.FileMode != evidence.Contract.FileMode {
		return errors.New("fleet persistence permissions are not qualified")
	}
	if !evidence.WriterLockHeld {
		return errors.New("fleet persistence writer lock is not held")
	}
	if evidence.AvailableBytes < evidence.Contract.DiskReserveBytes {
		return errors.New("fleet persistence disk reserve is unavailable")
	}
	if evidence.SchemaVersion != evidence.Contract.SchemaVersion || !evidence.MigrationComplete {
		return errors.New("fleet persistence schema or migration is not ready")
	}
	if !evidence.LastShutdownClean && !evidence.DirtyRecoveryVerified {
		return errors.New("fleet persistence dirty open has not been recovered")
	}
	return nil
}
