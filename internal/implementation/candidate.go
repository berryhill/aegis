package implementation

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SourceFile commits the type, mode and bytes, including untracked, ignored and
// generated inputs. Contents remain in private controller custody, never logs.
type SourceFile struct {
	Path    string `json:"path"`
	Mode    uint32 `json:"mode"`
	Content []byte `json:"content"`
}
type SourceInventory struct {
	Schema string       `json:"schema"`
	Files  []SourceFile `json:"files"`
	Digest string       `json:"digest"`
}

// Inventory extends the implementation kernel's root-relative, single-link
// reader. There is no selected-file fallback and no implicit ignored exclusion.
func Inventory(ctx context.Context, workspace string) (SourceInventory, error) {
	var result SourceInventory
	result.Schema = "aegis.contribution.source.v1"
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return result, err
	}
	defer root.Close()
	total := int64(0)
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, x error) error {
		if x != nil {
			return x
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		base := strings.ToLower(d.Name())
		if base == ".env" || strings.HasPrefix(base, ".env.") || base == "auth.json" || base == "credentials.json" || strings.HasSuffix(base, ".pem") || strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") {
			return errors.New("sensitive source input denied")
		}
		if d.IsDir() {
			return nil
		}
		info, x := root.Lstat(p)
		if x != nil || !info.Mode().IsRegular() || !singleLink(info) {
			return errors.New("nonregular source input denied")
		}
		if info.Size() > maxBytes || total+info.Size() > maxBytes || len(result.Files) >= 4096 {
			return errors.New("complete source inventory exceeds supported profile")
		}
		b, x := readRepositoryContextFile(root, p, info.Size())
		if x != nil {
			return x
		}
		if strings.Contains(string(b), "-----BEGIN "+"PRIVATE KEY-----") || strings.Contains(string(b), "-----BEGIN RSA "+"PRIVATE KEY-----") {
			return errors.New("private key input denied")
		}
		total += int64(len(b))
		result.Files = append(result.Files, SourceFile{p, uint32(info.Mode().Perm()), b})
		return nil
	})
	if err != nil {
		return SourceInventory{}, err
	}
	b, err := json.Marshal(result)
	if err != nil {
		return SourceInventory{}, err
	}
	result.Digest = digest(b)
	return result, nil
}

func (s SourceInventory) Materialize(destination string) error {
	if err := os.Mkdir(destination, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, f := range s.Files {
		if !filepath.IsLocal(f.Path) || filepath.Clean(f.Path) != f.Path {
			return errors.New("invalid inventory path")
		}
		if err := root.MkdirAll(filepath.Dir(f.Path), 0700); err != nil {
			return err
		}
		out, err := root.OpenFile(f.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(f.Mode))
		if err != nil {
			return err
		}
		_, err = out.Write(f.Content)
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
