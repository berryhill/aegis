// Package qualification freezes the reviewed persistence combinations for the
// MVI. It is a declarative qualification record, not host discovery: unknown
// planes, engines, versions, platforms, filesystems, and durability modes deny.
package qualification

import (
	"errors"
	"fmt"
	"time"
)

type Plane string

type Status string

const (
	PlaneSessionAuthority Plane = "session-authority"
	PlaneCredentials      Plane = "credential-custody"

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
)

// Contract is one complete reviewed storage combination. Engine-specific
// switches remain separate so that a setting meaningful to one engine cannot
// be mistaken for a generic durability promise.
type Contract struct {
	Plane         Plane
	Status        Status
	Backend       string
	ModulePath    string
	ModuleVersion string
	GOOS          string
	GOARCH        string
	Filesystem    string
	Authority     string
	DirectoryMode uint32
	FileMode      uint32

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
	return []Contract{qualified[PlaneSessionAuthority], qualified[PlaneCredentials]}
}

// Validate fails closed unless input exactly matches its reviewed baseline.
func Validate(contract Contract) error {
	baseline, err := Baseline(contract.Plane)
	if err != nil {
		return err
	}
	if contract != baseline {
		return errors.New("persistence engine, version, platform, filesystem, authority, or durability combination is not qualified")
	}
	return nil
}
