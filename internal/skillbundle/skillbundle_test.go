package skillbundle

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testRevision = "0123456789abcdef0123456789abcdef01234567"

func TestValidateRepositoryBundle(t *testing.T) {
	root := repositoryRoot(t)
	manifest, err := Validate(root)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if manifest.Bundle.Name != "aegis-hermes-skills" || len(manifest.Skills) == 0 {
		t.Fatalf("Validate() returned unexpected manifest: %#v", manifest.Bundle)
	}

	result, err := Evaluate(root, manifest)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.Cases < len(requiredEvaluationClasses) || result.Passed != result.Cases {
		t.Fatalf("Evaluate() result = %#v", result)
	}
}

func TestValidateFailsClosed(t *testing.T) {
	t.Run("trailing manifest document", func(t *testing.T) {
		root := copyFixture(t)
		manifestPath := filepath.Join(root, ManifestName)
		data := mustRead(t, manifestPath)
		mustWrite(t, manifestPath, append(data, []byte("{}\n")...))
		assertDenial(t, ValidateBundle(root), "manifest_malformed")
	})

	t.Run("tampered content", func(t *testing.T) {
		root := copyFixture(t)
		skillPath := filepath.Join(root, "skills", "aegis", "SKILL.md")
		mustWrite(t, skillPath, append(mustRead(t, skillPath), []byte("\nTampered.\n")...))
		assertDenial(t, ValidateBundle(root), "file_digest_mismatch")
	})

	t.Run("secret shaped content", func(t *testing.T) {
		root := copyFixture(t)
		skillPath := filepath.Join(root, "skills", "aegis", "SKILL.md")
		content := append(mustRead(t, skillPath), []byte("\nSynthetic canary: "+"AKIA"+strings.Repeat("A", 16)+"\n")...)
		mustWrite(t, skillPath, content)
		rehashFirstSkill(t, root, content)
		assertDenial(t, ValidateBundle(root), "secret_literal")
	})

	t.Run("missing denial coverage", func(t *testing.T) {
		root := copyFixture(t)
		var suite EvaluationSuite
		mustDecode(t, mustRead(t, filepath.Join(root, EvaluationsName)), &suite)
		suite.Cases = suite.Cases[:len(suite.Cases)-1]
		mustWriteJSON(t, filepath.Join(root, EvaluationsName), suite)
		assertDenial(t, ValidateBundle(root), "evaluation_coverage_missing")
	})

	t.Run("symlinked skill content", func(t *testing.T) {
		root := copyFixture(t)
		skillPath := filepath.Join(root, "skills", "aegis", "SKILL.md")
		if err := os.Remove(skillPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(repositoryRoot(t), "skills", "aegis", "SKILL.md"), skillPath); err != nil {
			t.Fatal(err)
		}
		assertDenial(t, ValidateBundle(root), "unsafe_file_type")
	})
}

func TestValidateDependencyGraph(t *testing.T) {
	skill := func(slug string, dependencies ...string) Skill {
		return Skill{Slug: slug, Path: "skills/" + slug, Dependencies: dependencies}
	}
	t.Run("self cycle", func(t *testing.T) {
		err := validateDependencyGraph(map[string]Skill{"aegis": skill("aegis", "aegis")})
		assertDenial(t, err, "dependency_cycle")
	})
	t.Run("multi node cycle", func(t *testing.T) {
		err := validateDependencyGraph(map[string]Skill{
			"aegis":           skill("aegis", "aegis-secondary"),
			"aegis-secondary": skill("aegis-secondary", "aegis"),
		})
		assertDenial(t, err, "dependency_cycle")
	})
	t.Run("acyclic graph", func(t *testing.T) {
		err := validateDependencyGraph(map[string]Skill{
			"aegis":           skill("aegis", "aegis-secondary"),
			"aegis-secondary": skill("aegis-secondary"),
		})
		if err != nil {
			t.Fatalf("validateDependencyGraph() error = %v", err)
		}
	})
}

func TestBuildAndVerifyArchiveDeterministically(t *testing.T) {
	root := repositoryRoot(t)
	one := filepath.Join(t.TempDir(), "one.tar.gz")
	two := filepath.Join(t.TempDir(), "two.tar.gz")
	digestOne, err := BuildArchive(root, one, "1.2.3", testRevision)
	if err != nil {
		t.Fatalf("BuildArchive(one) error = %v", err)
	}
	digestTwo, err := BuildArchive(root, two, "1.2.3", testRevision)
	if err != nil {
		t.Fatalf("BuildArchive(two) error = %v", err)
	}
	if digestOne != digestTwo || string(mustRead(t, one)) != string(mustRead(t, two)) {
		t.Fatal("BuildArchive() output is not deterministic")
	}
	manifest, err := VerifyArchive(one, testRevision)
	if err != nil {
		t.Fatalf("VerifyArchive() error = %v", err)
	}
	if manifest.Bundle.Version != "1.2.3" || manifest.Bundle.SourceRevision != testRevision {
		t.Fatalf("verified provenance = %#v", manifest.Bundle)
	}
}

func TestVerifyArchiveRequiresExactImmutableProvenance(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if _, err := BuildArchive(repositoryRoot(t), archive, "1.2.3", testRevision); err != nil {
		t.Fatal(err)
	}

	t.Run("mismatched expected revision", func(t *testing.T) {
		_, err := VerifyArchive(archive, "abcdef0123456789abcdef0123456789abcdef01")
		assertDenial(t, err, "archive_provenance_mismatch")
	})
	t.Run("mutable embedded revision", func(t *testing.T) {
		mutable := filepath.Join(t.TempDir(), "mutable.tar.gz")
		rewriteArchiveMember(t, archive, mutable, "skills/aegis-skills.json", func(content []byte) []byte {
			var manifest Manifest
			mustDecode(t, content, &manifest)
			manifest.Bundle.SourceRevision = "repository"
			data, err := json.MarshalIndent(manifest, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			return append(data, '\n')
		})
		_, err := VerifyArchive(mutable, testRevision)
		assertDenial(t, err, "archive_provenance_mismatch")
	})
	t.Run("mutable expected revision", func(t *testing.T) {
		_, err := VerifyArchive(archive, "repository")
		assertDenial(t, err, "invalid_source_revision")
	})
}

func TestVerifyArchiveRejectsUndeclaredMember(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bundle.tar.gz")
	if _, err := BuildArchive(repositoryRoot(t), archive, "1.2.3", testRevision); err != nil {
		t.Fatal(err)
	}
	withRogue := filepath.Join(t.TempDir(), "rogue.tar.gz")
	addArchiveMember(t, archive, withRogue, "aegis-skills-v1.2.3/README.md", []byte("not declared\n"))
	_, err := VerifyArchive(withRogue, testRevision)
	assertDenial(t, err, "undeclared_archive_member")
}

func ValidateBundle(root string) error {
	_, err := Validate(root)
	return err
}

func assertDenial(t *testing.T, err error, code string) {
	t.Helper()
	var denial *Denial
	if !errors.As(err, &denial) {
		t.Fatalf("error = %v, want Denial code %q", err, code)
	}
	if denial.Code != code {
		t.Fatalf("denial code = %q, want %q (error: %v)", denial.Code, code, err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func copyFixture(t *testing.T) string {
	t.Helper()
	destination := t.TempDir()
	source := filepath.Join(repositoryRoot(t), "skills")
	err := filepath.WalkDir(source, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, "skills", rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return os.WriteFile(target, mustRead(t, current), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return destination
}

func rehashFirstSkill(t *testing.T, root string, content []byte) {
	t.Helper()
	manifestPath := filepath.Join(root, ManifestName)
	var manifest Manifest
	mustDecode(t, mustRead(t, manifestPath), &manifest)
	file := &manifest.Skills[0].Files[0]
	file.SHA256 = sha256Digest(content)
	file.Size = int64(len(content))
	manifest.Skills[0].ContentDigest = fileSetDigest(manifest.Skills[0].Files)
	manifest.Bundle.ContentDigest = bundleDigest(manifest.Skills)
	mustWriteJSON(t, manifestPath, manifest)
}

func addArchiveMember(t *testing.T, source, destination, name string, content []byte) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	gzIn, err := gzip.NewReader(in)
	if err != nil {
		t.Fatal(err)
	}
	defer gzIn.Close()
	out, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	gzOut := gzip.NewWriter(out)
	tarOut := tar.NewWriter(gzOut)
	tarIn := tar.NewReader(gzIn)
	for {
		header, nextErr := tarIn.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		copyHeader := *header
		if err := tarOut.WriteHeader(&copyHeader); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(tarOut, tarIn); err != nil {
			t.Fatal(err)
		}
	}
	header := &tar.Header{Name: name, Mode: archiveMode, Size: int64(len(content)), Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0)}
	if err := tarOut.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tarOut.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarOut.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzOut.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func rewriteArchiveMember(t *testing.T, source, destination, member string, rewrite func([]byte) []byte) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	gzIn, err := gzip.NewReader(in)
	if err != nil {
		t.Fatal(err)
	}
	defer gzIn.Close()
	out, err := os.Create(destination)
	if err != nil {
		t.Fatal(err)
	}
	gzOut := gzip.NewWriter(out)
	tarOut := tar.NewWriter(gzOut)
	tarIn := tar.NewReader(gzIn)
	found := false
	for {
		header, nextErr := tarIn.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		content, readErr := io.ReadAll(tarIn)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.HasSuffix(header.Name, "/"+member) {
			content = rewrite(content)
			found = true
		}
		copyHeader := *header
		copyHeader.Size = int64(len(content))
		if err := tarOut.WriteHeader(&copyHeader); err != nil {
			t.Fatal(err)
		}
		if _, err := tarOut.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if !found {
		t.Fatalf("archive member %q not found", member)
	}
	if err := tarOut.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzOut.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustWrite(t *testing.T, file string, data []byte) {
	t.Helper()
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustDecode(t *testing.T, data []byte, value any) {
	t.Helper()
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func mustWriteJSON(t *testing.T, file string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, file, append(data, '\n'))
}
