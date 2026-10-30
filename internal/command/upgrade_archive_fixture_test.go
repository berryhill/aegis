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
func TestUpgradeFixtureArchiveCacheIsBoundedAndReturnsValueCopies(t *testing.T) {
	var cache upgradeFixtureArchiveCache
	for _, binary := range [][]byte{[]byte("code1"), []byte("code2"), []byte("code3"), []byte("code4")} {
		first, hit, err := cache.pack(binary)
		if err != nil || hit || first.validate(binary) != nil {
			t.Fatal("first source was not packed normally", hit, err)
		}
		first.data = "caller mutation"
		first.binaryDigest[0] ^= 1
		again, hit, err := cache.pack(binary)
		if err != nil || !hit || again.validate(binary) != nil {
			t.Fatal("cached input or value-copy isolation changed", hit, err)
		}
	}
	if len(cache.values) != 4 || cache.retained > 64<<20 {
		t.Fatal("retained archive inputs exceeded their bounds")
	}
	for i := 0; i < 2; i++ {
		archive, hit, err := cache.pack([]byte("fifth code"))
		if err != nil || hit || archive.validate([]byte("fifth code")) != nil || len(cache.values) != 4 {
			t.Fatal("uncached input failed or escaped retention bound", hit, err)
		}
	}
	full := upgradeFixtureArchiveCache{retained: 64 << 20}
	if archive, hit, err := full.pack([]byte("uncached valid code")); err != nil || hit || len(full.values) != 0 || archive.validate([]byte("uncached valid code")) != nil {
		t.Fatal("retention byte limit skipped real packing or retained more data", hit, err)
	}
}

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
