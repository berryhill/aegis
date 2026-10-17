package update

import (
	"bytes"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Inspect the downloaded executable without executing its code or opening state.
// These are checksum-bound build claims, not independent signature verification.
func validateCandidate(binary []byte, version, goos, goarch string) error {
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		return fmt.Errorf("candidate executable build information: %w", err)
	}
	if info.Path != "github.com/berryhill/aegis/cmd/aegis" || info.Main.Path != "github.com/berryhill/aegis" {
		return errors.New("candidate is not an Aegis executable")
	}
	settings := make(map[string]string)
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	if settings["GOOS"] != goos || settings["GOARCH"] != goarch || settings["-buildmode"] != "exe" {
		return errors.New("candidate executable platform mismatch")
	}
	if goos == "linux" {
		f, err := elf.NewFile(bytes.NewReader(binary))
		if err != nil {
			return fmt.Errorf("candidate ELF: %w", err)
		}
		defer f.Close()
		machine := elf.EM_X86_64
		if goarch == "arm64" {
			machine = elf.EM_AARCH64
		}
		if f.Machine != machine || (f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN) {
			return errors.New("candidate ELF platform mismatch")
		}
	} else {
		f, err := macho.NewFile(bytes.NewReader(binary))
		if err != nil {
			return fmt.Errorf("candidate Mach-O: %w", err)
		}
		defer f.Close()
		cpu := macho.CpuAmd64
		if goarch == "arm64" {
			cpu = macho.CpuArm64
		}
		if f.Cpu != cpu || f.Type != macho.TypeExec {
			return errors.New("candidate Mach-O platform mismatch")
		}
	}
	// Release builds use these exact, unquoted -X assignments. Reject duplicate
	// bindings and unsupported quoting rather than guessing linker semantics.
	bindings := make(map[string]string)
	fields := strings.Fields(settings["-ldflags"])
	for i := 0; i < len(fields); i++ {
		if fields[i] != "-X" {
			continue
		}
		i++
		if i == len(fields) {
			return errors.New("candidate has malformed release bindings")
		}
		key, value, ok := strings.Cut(fields[i], "=")
		if !ok || strings.ContainsAny(value, "\"'") {
			return errors.New("candidate has malformed release bindings")
		}
		if _, exists := bindings[key]; exists {
			return errors.New("candidate has ambiguous release bindings")
		}
		bindings[key] = value
	}
	const prefix = "github.com/berryhill/aegis/internal/buildinfo."
	if normalize(bindings[prefix+"Version"]) != version {
		return errors.New("candidate embedded version does not match release")
	}
	revision := bindings[prefix+"SourceRevision"]
	if _, err := hex.DecodeString(revision); err != nil || len(revision) != 40 || strings.ToLower(revision) != revision {
		return errors.New("candidate embedded source revision unavailable or malformed")
	}
	if vcs := settings["vcs.revision"]; vcs != "" && vcs != revision {
		return errors.New("candidate source revision disagrees with VCS provenance")
	}
	if settings["vcs.modified"] == "true" {
		return errors.New("candidate was built from modified source")
	}
	return nil
}
