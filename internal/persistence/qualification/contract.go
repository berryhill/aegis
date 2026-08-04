// Package qualification defines the reviewed persistence envelope. It does
// not open a database or infer support from the host: callers must present an
// exact qualified combination and unknown combinations are denied.
package qualification

import (
	"errors"
	"time"
)

const (
	BackendBBolt       = "bbolt"
	BBoltModulePath    = "go.etcd.io/bbolt"
	BBoltModuleVersion = "v1.5.0"

	QualifiedGOOS       = "linux"
	QualifiedGOARCH     = "amd64"
	QualifiedFilesystem = "ext4"
	AuthorityModel      = "single-aegis-process"
)

// Contract is the complete reviewed persistence combination. These fields
// are deliberately concrete rather than ranges or capability claims.
type Contract struct {
	Backend       string
	ModulePath    string
	ModuleVersion string
	GOOS          string
	GOARCH        string
	Filesystem    string
	Authority     string
	DirectoryMode uint32
	FileMode      uint32
	LockTimeout   time.Duration
	SingleWriter  bool
	NoSync        bool
	NoGrowSync    bool
}

// Baseline returns the only persistence combination qualified for the MVP.
func Baseline() Contract {
	return Contract{
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
		SingleWriter:  true,
	}
}

// Validate fails closed unless the complete input exactly matches the
// reviewed baseline. Adding a dependency, platform, filesystem, or authority
// mode therefore requires an explicit qualification change.
func Validate(contract Contract) error {
	if contract != Baseline() {
		return errors.New("persistence dependency, platform, filesystem, or authority combination is not qualified")
	}
	return nil
}
