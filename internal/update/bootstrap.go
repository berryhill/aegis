package update

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"errors"
	"os"
	"strings"
)

func (u *Updater) InstalledBootstrap() bool { return u.installedDigest != nil }

// ForInstalled binds release discovery to inspected installed bytes, not the
// newer coordinator's version. The command must first validate service ownership.
func (u *Updater) ForInstalled(path string) (*Updater, error) {
	sealedInfo, data, err := snapshotBytes(path)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	info, err := buildinfo.Read(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	version := strings.TrimPrefix(info.Main.Version, "v")
	if version == "(devel)" {
		version = ""
	}
	for _, setting := range info.Settings {
		if setting.Key != "-ldflags" {
			continue
		}
		for _, field := range strings.Fields(setting.Value) {
			if strings.HasPrefix(field, "github.com/berryhill/aegis/internal/buildinfo.Version=") {
				version = strings.TrimPrefix(field, "github.com/berryhill/aegis/internal/buildinfo.Version=")
			}
		}
	}
	if version == "" {
		return nil, errors.New("installed release version unavailable")
	}
	if _, err = inspectCandidate(data, normalize(version), u.GOOS, u.GOARCH); err != nil {
		return nil, err
	}
	afterInfo, after, err := snapshot(path)
	if err != nil || !os.SameFile(sealedInfo, afterInfo) || sealedInfo.Mode() != afterInfo.Mode() || after != digest {
		return nil, errors.New("installed image changed during bootstrap inspection")
	}
	copy := *u
	copy.CurrentVersion = version
	copy.Executable = func() (string, error) { return path, nil }
	copy.installedDigest = &digest
	copy.installedInfo = sealedInfo
	return &copy, nil
}
