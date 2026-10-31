package delivery

import (
	"bytes"
	"context"
	"crypto/sha1" // Git's canonical SHA-1 object identity, not an authority digest.
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	TransferGraph  Operation = "github.git_graph.v1"
	UploadBlob     Operation = "github.git_blob.v1"
	UploadTree     Operation = "github.git_tree.v1"
	UploadCommit   Operation = "github.git_commit.v1"
	maxObjectBytes           = 512 << 10
	maxGraphBytes            = 16 << 20
	maxObjects               = 2048
	// History admits 512 intent chains, including eight non-object slots:
	// Commit, TransferGraph, Publish, PullRequest, UpdatePullRequest,
	// CheckDelivery, Merge and ReconcileIssue. The tighter existing 400-fact
	// readback limit must also fit a full graph plus the worst-case eight
	// identity, absence, parent and final-read facts for UploadCommit.
	// Enforce the smaller envelope BEFORE uploads; neither limit is widened.
	// Stable intent slots and the pinned TransferGraph prevent repair growth.
	maxPublicationObjects = 400 - 8
)

// GitObject is an operational projection of already accepted local Git bytes.
// It conveys no acceptance or source custody; Source remains the sole authority.
type GitObject struct {
	Kind, OID string
	Raw       []byte
}
type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}
type gitPerson struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Date  string `json:"date"`
}
type gitCommitPayload struct {
	Message   string    `json:"message"`
	Tree      string    `json:"tree"`
	Parents   []string  `json:"parents"`
	Author    gitPerson `json:"author"`
	Committer gitPerson `json:"committer"`
}

func objectOID(kind string, raw []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s %d\x00", kind, len(raw))
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}
func objectOp(o GitObject) Operation {
	switch o.Kind {
	case "blob":
		return UploadBlob
	case "tree":
		return UploadTree
	case "commit":
		return UploadCommit
	}
	return ""
}
func objectValid(o GitObject) bool {
	return objectOp(o) != "" && len(o.Raw) <= maxObjectBytes && sha.MatchString(o.OID) && objectOID(o.Kind, o.Raw) == o.OID
}
func privatePath(s string) bool {
	for _, p := range strings.Split(strings.ToLower(s), "/") {
		if p == ".env" || strings.HasPrefix(p, ".env.") || p == ".ssh" || p == ".git" || p == ".aws" || p == "credentials" || strings.HasSuffix(p, ".pem") || strings.HasSuffix(p, ".key") {
			return true
		}
	}
	return false
}
func secretBytes(b []byte) bool {
	return publicationCredential.Match(b) || closing.Match(b)
}
func treeEntries(raw []byte) ([]treeEntry, error) {
	entries := []treeEntry{}
	previous := ""
	seen := map[string]bool{}
	for len(raw) > 0 {
		space := bytes.IndexByte(raw, ' ')
		zero := bytes.IndexByte(raw, 0)
		if space < 1 || zero <= space+1 || len(raw) < zero+21 {
			return nil, ErrDenied
		}
		mode, name := string(raw[:space]), string(raw[space+1:zero])
		typ := "blob"
		switch mode {
		case "40000":
			mode = "040000"
			typ = "tree"
		case "100644", "100755":
		default:
			return nil, ErrDenied
		}
		if !utf8.ValidString(name) || !safePath(name) || strings.Contains(name, "/") || privatePath(name) || seen[name] {
			return nil, ErrDenied
		}
		key := name
		if typ == "tree" {
			key += "/"
		}
		if previous != "" && previous >= key {
			return nil, ErrDenied
		}
		previous = key
		seen[name] = true
		entries = append(entries, treeEntry{name, mode, typ, hex.EncodeToString(raw[zero+1 : zero+21])})
		raw = raw[zero+21:]
		if len(entries) > maxObjects {
			return nil, ErrDenied
		}
	}
	return entries, nil
}
func person(line string) (gitPerson, error) {
	end := strings.LastIndex(line, "> ")
	start := strings.LastIndex(line, " <")
	if start < 1 || end < start {
		return gitPerson{}, ErrDenied
	}
	id := Identity{line[:start], line[start+2 : end]}
	fields := strings.Fields(line[end+2:])
	if !identityOK(id) || len(fields) != 2 || len(fields[1]) != 5 {
		return gitPerson{}, ErrDenied
	}
	sec, e := strconv.ParseInt(fields[0], 10, 64)
	if e != nil {
		return gitPerson{}, ErrDenied
	}
	t, e := time.Parse("-0700", fields[1])
	if e != nil {
		return gitPerson{}, ErrDenied
	}
	_, offset := t.Zone()
	date := time.Unix(sec, 0).In(time.FixedZone("", offset)).Format(time.RFC3339)
	if fmt.Sprintf("%s <%s> %d %s", id.Name, id.Email, sec, fields[1]) != line {
		return gitPerson{}, ErrDenied
	}
	return gitPerson{id.Name, id.Email, date}, nil
}
func commitPayload(o GitObject) (gitCommitPayload, error) {
	var p gitCommitPayload
	split := bytes.SplitN(o.Raw, []byte("\n\n"), 2)
	if len(split) != 2 || !utf8.Valid(o.Raw) || bytes.IndexByte(o.Raw, 0) >= 0 {
		return p, ErrDenied
	}
	lines := strings.Split(string(split[0]), "\n")
	// Positive four-header profile: explicitly reject merge, signatures, encoding,
	// extensions and multiline headers, rather than publishing a different commit.
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "tree ") || !strings.HasPrefix(lines[1], "parent ") || !strings.HasPrefix(lines[2], "author ") || !strings.HasPrefix(lines[3], "committer ") {
		return p, ErrDenied
	}
	p.Tree = strings.TrimPrefix(lines[0], "tree ")
	parent := strings.TrimPrefix(lines[1], "parent ")
	if !sha.MatchString(p.Tree) || !sha.MatchString(parent) {
		return p, ErrDenied
	}
	p.Parents = []string{parent}
	var e error
	p.Author, e = person(strings.TrimPrefix(lines[2], "author "))
	if e != nil {
		return p, e
	}
	p.Committer, e = person(strings.TrimPrefix(lines[3], "committer "))
	if e != nil {
		return p, e
	}
	p.Message = string(split[1])
	if p.Message == "" || secretBytes(split[1]) {
		return p, ErrDenied
	}
	return p, nil
}

// ExtractObjects only reads exact local OIDs through closed cat-file plumbing.
// No rev expressions, URLs, shell, hooks, network or credential environment.
// The accepted source owner must hold local custody for the entire extraction.
func (g *Git) ExtractObjects(ctx context.Context, head, tree, parent string) ([]GitObject, error) {
	if !sha.MatchString(head) || !sha.MatchString(tree) || !sha.MatchString(parent) {
		return nil, ErrDenied
	}
	var out []GitObject
	seen := map[string]bool{}
	total := 0
	var visit func(string, string, int) error
	visit = func(oid, kind string, depth int) error {
		if depth > 32 || len(out) >= maxObjects {
			return ErrDenied
		}
		if seen[oid] {
			return nil
		}
		size, e := g.run(ctx, nil, nil, "cat-file", "-s", oid)
		if e != nil {
			return ErrDenied
		}
		n, e := strconv.Atoi(strings.TrimSpace(string(size)))
		if e != nil || n < 0 || n > maxObjectBytes || total+n > maxGraphBytes {
			return ErrDenied
		}
		total += n
		raw, e := g.run(ctx, nil, nil, "cat-file", kind, oid)
		if e != nil {
			return ErrDenied
		}
		o := GitObject{kind, oid, raw}
		if !objectValid(o) {
			return ErrDenied
		}
		seen[oid] = true
		switch kind {
		case "tree":
			entries, e := treeEntries(raw)
			if e != nil {
				return e
			}
			for _, entry := range entries {
				if e = visit(entry.SHA, entry.Type, depth+1); e != nil {
					return e
				}
			}
		case "blob":
			if secretBytes(raw) {
				return ErrDenied
			}
		case "commit":
			p, e := commitPayload(o)
			if e != nil || p.Tree != tree || p.Parents[0] != parent {
				return ErrDenied
			}
			if e = visit(tree, "tree", depth+1); e != nil {
				return e
			}
		default:
			return ErrDenied
		}
		out = append(out, o)
		return nil
	}
	if e := visit(head, "commit", 0); e != nil {
		return nil, e
	}
	return out, nil
}
func validateGraph(p Plan) error {
	if len(p.Objects) == 0 || len(p.Objects) > maxPublicationObjects {
		return ErrDenied
	}
	seen := map[string]string{}
	total := 0
	depths := map[string]int{}
	for n, o := range p.Objects {
		total += len(o.Raw)
		if total > maxGraphBytes || !objectValid(o) || seen[o.OID] != "" {
			return ErrDenied
		}
		switch o.Kind {
		case "blob":
			if secretBytes(o.Raw) {
				return ErrDenied
			}
		case "tree":
			entries, e := treeEntries(o.Raw)
			if e != nil {
				return e
			}
			for _, entry := range entries {
				if seen[entry.SHA] != entry.Type {
					return ErrDenied
				}
				if depths[entry.SHA]+1 > depths[o.OID] {
					depths[o.OID] = depths[entry.SHA] + 1
				}
			}
		case "commit":
			c, e := commitPayload(o)
			if e != nil || n != len(p.Objects)-1 || o.OID != p.Head || c.Tree != p.Commit.Tree || c.Parents[0] != p.BaseHead || seen[c.Tree] != "tree" {
				return ErrDenied
			}
		}
		if depths[o.OID] > 32 {
			return ErrDenied
		}
		seen[o.OID] = o.Kind
	}
	// Every uploaded object must be reachable from the accepted commit tree.
	byID := map[string]GitObject{}
	for _, o := range p.Objects {
		byID[o.OID] = o
	}
	reachable := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		o := byID[id]
		if o.Kind == "tree" {
			entries, _ := treeEntries(o.Raw)
			for _, entry := range entries {
				walk(entry.SHA)
			}
		}
	}
	walk(p.Commit.Tree)
	reachable[p.Head] = true
	if len(reachable) != len(p.Objects) {
		return ErrDenied
	}
	if p.Objects[len(p.Objects)-1].Kind != "commit" {
		return ErrDenied
	}
	return nil
}
