package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/berryhill/aegis/internal/skillbundle"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: go run ./internal/skillbundle/cmd validate ROOT | evaluate ROOT | build ROOT DIST VERSION SOURCE_REVISION | verify ARCHIVE SOURCE_REVISION")
	}
	switch args[0] {
	case "validate":
		if len(args) != 2 {
			return fmt.Errorf("validate requires ROOT")
		}
		manifest, err := skillbundle.Validate(args[1])
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"status": "valid", "bundle": manifest.Bundle.Name, "version": manifest.Bundle.Version, "skills": len(manifest.Skills)})
	case "evaluate":
		if len(args) != 2 {
			return fmt.Errorf("evaluate requires ROOT")
		}
		manifest, err := skillbundle.Validate(args[1])
		if err != nil {
			return err
		}
		result, err := skillbundle.Evaluate(args[1], manifest)
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"status": "passed", "cases": result.Cases, "passed": result.Passed})
	case "build":
		if len(args) != 5 {
			return fmt.Errorf("build requires ROOT, DIST, VERSION, and SOURCE_REVISION")
		}
		name, err := skillbundle.ArchiveFilename(args[3])
		if err != nil {
			return err
		}
		destination := filepath.Join(args[2], name)
		digest, err := skillbundle.BuildArchive(args[1], destination, args[3], args[4])
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"status": "built", "archive": destination, "sha256": digest})
	case "verify":
		if len(args) != 3 {
			return fmt.Errorf("verify requires ARCHIVE and SOURCE_REVISION")
		}
		manifest, err := skillbundle.VerifyArchive(args[1], args[2])
		if err != nil {
			return err
		}
		return printJSON(map[string]any{"status": "verified", "version": manifest.Bundle.Version, "content_digest": manifest.Bundle.ContentDigest})
	default:
		return fmt.Errorf("unknown action %q", args[0])
	}
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
