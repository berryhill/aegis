//go:build linux

package command

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"io"
	"testing"
)

// Codec/isolation proof only. Actual executable admission remains covered by
// TestInstalledUpgradeFullCommand through the production candidate reader.
func TestUpgradeFixtureArchiveIsImmutableAndSourceBound(t *testing.T) {
	binary := []byte("codec fixture input, not execution evidence")
	original := append([]byte(nil), binary...)
	archive, err := packUpgradeFixtureArchive(binary)
	if err != nil {
		t.Fatal(err)
	}
	again, err := packUpgradeFixtureArchive(binary)
	if err != nil || archive != again {
		t.Fatal("identical inputs produced different archive commitments", err)
	}
	if err := archive.validate(binary); err != nil {
		t.Fatal(err)
	}
	copy := archive
	copy.binaryDigest[0] ^= 1
	if copy.validate(binary) == nil || archive.validate(binary) != nil {
		t.Fatal("archive value mutation escaped its copy or lost source binding")
	}
	copy = archive
	copy.data += "substitution"
	if copy.validate(binary) == nil || archive.validate(binary) != nil {
		t.Fatal("changed packed bytes were accepted or changed the original")
	}
	binary[0] ^= 1
	if archive.validate(binary) == nil || archive.validate(original) != nil {
		t.Fatal("changed source bytes reused stale packed input")
	}
	if sha256.Sum256([]byte(archive.data)) != archive.checksum {
		t.Fatal("packed checksum does not bind actual bytes")
	}
	gz, err := gzip.NewReader(bytes.NewBufferString(archive.data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	header, err := reader.Next()
	if err != nil || header.Name != "aegis" || header.Mode != 0755 || header.Typeflag != tar.TypeReg || header.Size != int64(len(original)) {
		t.Fatal("fixture archive format changed", err)
	}
	content, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(content, original) {
		t.Fatal("archive does not contain the exact candidate input", err)
	}
	if _, err := reader.Next(); err != io.EOF {
		t.Fatal("fixture archive gained another entry", err)
	}
}
