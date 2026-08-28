package slash

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBoundedAgentInputEnforcesRegularFileAndActualReadLimit(t *testing.T) {
	root := t.TempDir()
	exactPath := filepath.Join(root, "exact")
	exact := bytes.Repeat([]byte("a"), maximumAgentInputBytes)
	if err := os.WriteFile(exactPath, exact, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := readBoundedAgentInput(exactPath)
	if err != nil {
		t.Fatalf("read exact-limit input: %v", err)
	}
	if !bytes.Equal(got, exact) {
		t.Fatal("exact-limit input changed during read")
	}

	overPath := filepath.Join(root, "over")
	if err = os.WriteFile(overPath, bytes.Repeat([]byte("b"), maximumAgentInputBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readBoundedAgentInput(overPath); err == nil || !strings.Contains(err.Error(), "no larger than") {
		t.Fatalf("over-limit input was not rejected: %v", err)
	}

	if _, err = readBoundedAgentInput(root); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("non-regular input was not rejected: %v", err)
	}
}

func TestReadBoundedAgentFileUsesOpenedObjectAfterPathReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent-input")
	const original = "opened object"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if err = os.Rename(path, filepath.Join(root, "original")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, bytes.Repeat([]byte("x"), maximumAgentInputBytes+1), 0600); err != nil {
		t.Fatal(err)
	}

	got, err := readBoundedAgentFile(file, path)
	if err != nil {
		t.Fatalf("read opened object after path replacement: %v", err)
	}
	if string(got) != original {
		t.Fatalf("read replacement path instead of opened object: %q", got)
	}
}

func TestReadBoundedAgentFileRejectsOpenedFileThatGrowsPastLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-input")
	if err := os.WriteFile(path, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	if err = os.WriteFile(path, bytes.Repeat([]byte("x"), maximumAgentInputBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readBoundedAgentFile(file, path); err == nil || !strings.Contains(err.Error(), "no larger than") {
		t.Fatalf("opened file growth past limit was not rejected: %v", err)
	}
}
