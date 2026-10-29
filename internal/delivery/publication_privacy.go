package delivery

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/berryhill/aegis/internal/contribution"
)

// PublicationPrivacy is a closed, stateless content guard. It has no runtime
// overrides, ambient secret access, filesystem access or exception mechanism.
// Source.WithAccepted remains responsible for authenticated acceptance, frozen
// candidate custody and ProjectAccepted's complete inventory/scope validation.
// Pattern detection cannot prove absence of arbitrary unrecognized secret values.
type PublicationPrivacy struct{}

type PublicationPrivacyError string

func (e PublicationPrivacyError) Error() string { return string(e) }
func (e PublicationPrivacyError) Unwrap() error { return ErrDenied }

const (
	ErrPublicationAudience   PublicationPrivacyError = "publication_scope_denied"
	ErrPublicationContent    PublicationPrivacyError = "publication_content_denied"
	ErrPublicationProjection PublicationPrivacyError = "publication_projection_denied"
)

var publicationCredential = regexp.MustCompile(`(?im)(-----BEGIN (RSA |EC |DSA |OPENSSH |ENCRYPTED )?PRIVATE KEY-----|github_pat_[a-z0-9_]{82}|gh[pousr]_[a-z0-9]{36}|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|xox[baprs]-[a-z0-9-]{20,}|sk-(live-|proj-)[a-z0-9_-]{20,}|bearer[ \t]+[a-z0-9._/-]{20,}|(password|passwd|secret|token|api[ _-]?key|access[ _-]?key)["' \t]*[:=][ \t]*["'][a-z0-9_./+~-]{8,}["']|^[ \t]*(password|passwd|secret|token|api[ _-]?key|access[ _-]?key)[ \t]*=[ \t]*[a-z0-9_./+~-]{8,}[ \t]*$)`)

// '=' delimits assignments, except for at most two trailing padding bytes.
// Never try arbitrary suffixes: disjoint matches bound tokenization to O(n).
var publicationEncoded = regexp.MustCompile(`[A-Za-z0-9_+/-]{16,}={0,2}`)

func publicationPath(p string) bool {
	if !utf8.ValidString(p) || !safePath(p) || privatePath(p) || len(p) > 4096 {
		return false
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, part := range strings.Split(strings.ToLower(p), "/") {
		if part == ".hermes" || part == ".aegis" || part == ".gnupg" || part == "secrets" {
			return false
		}
	}
	return true
}

// inspect decodes at most two encoding layers, with a shared bounded work budget.
func publicationInspect(ctx context.Context, b []byte, depth int, budget *int) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	*budget -= len(b)
	if *budget < 0 || len(b) > maxObjectBytes || !utf8.Valid(b) {
		return ErrPublicationContent
	}
	for _, r := range string(b) {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return ErrPublicationContent
		}
	}
	if publicationCredential.Match(b) || closing.Match(b) {
		return ErrPublicationContent
	}
	if depth == 2 {
		return nil
	}
	if bytes.ContainsRune(b, '%') {
		if s, e := url.QueryUnescape(string(b)); e == nil && s != string(b) {
			if e = publicationInspect(ctx, []byte(s), depth+1, budget); e != nil {
				return e
			}
		}
	}
	for _, token := range publicationEncoded.FindAll(b, -1) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Hex can also be valid base64; inspect both interpretations. Choose one
		// base64 profile rather than decoding identical standard/URL forms twice.
		// Charge attempted work before allocation, including invalid encodings.
		encoding := base64.StdEncoding
		if bytes.ContainsAny(token, "-_") {
			encoding = base64.URLEncoding
		}
		if !bytes.ContainsRune(token, '=') {
			encoding = encoding.WithPadding(base64.NoPadding)
		}
		decoders := []func(string) ([]byte, error){hex.DecodeString, encoding.DecodeString}
		for _, decode := range decoders {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			*budget -= len(token)
			if *budget < 0 {
				return ErrPublicationContent
			}
			decoded, e := decode(string(token))
			if e != nil || len(decoded) == 0 {
				continue
			}
			if publicationCredential.Match(decoded) {
				return ErrPublicationContent
			}
			if utf8.Valid(decoded) {
				if e = publicationInspect(ctx, decoded, depth+1, budget); e != nil {
					return e
				}
			}
		}
	}
	return nil
}

func (PublicationPrivacy) Review(ctx context.Context, i Intent, p Plan) error {
	if ctx == nil {
		return ErrPublicationProjection
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Bound before hashing or semantic decoding, including malformed graphs.
	if i.Operation == Commit || len(p.Objects) == 0 || len(p.Objects) > maxObjects || len(p.Commit.Files) > 4096 {
		return ErrPublicationProjection
	}
	total := 0
	for _, o := range p.Objects {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(o.Raw) > maxObjectBytes {
			return ErrPublicationContent
		}
		total += len(o.Raw)
		if total > maxGraphBytes {
			return ErrPublicationContent
		}
	}
	if !roleDigest.MatchString(p.Candidate) || len(p.RequiredChecks) > 100 || len(p.RequiredReviewers) > 100 || len(p.PostMergeChecks) > 100 {
		return ErrPublicationProjection
	}
	if len(p.PRTitle) > 256 || len(p.PRBody) > 32768 || len(p.Commit.Message) > 16384 {
		return ErrPublicationContent
	}
	if ValidateProjectedIntent(p, i) != nil || validateGraph(p) != nil {
		return ErrPublicationProjection
	}
	// The authenticated seal must name this exact GitHub repository and explicitly
	// permit publication. Public-reader source provenance alone grants no rights.
	c := p.Intake
	if _, e := contribution.Marshal(c); e != nil || c.Digest != p.Contract || c.RunID != p.Run || c.Repository != p.Repository || c.Source.RepositoryID != p.RepositoryID || c.Base != p.BaseHead || !permits(c.Effect, i.Operation) || !permits(p.Delivery, i.Operation) || !contribution.DeliveryNarrows(c.Effect, p.Delivery) || c.Source.ObservedAt.IsZero() {
		return ErrPublicationAudience
	}
	// The existing authenticated seal selects the publication target. This guard
	// adds no authority: fresh effect admission and credential custody independently
	// enforce their rights ceilings, as does the source owner's current review.
	return inspectPublication(ctx, p)
}

func inspectPublication(ctx context.Context, p Plan) error {
	if ctx == nil || len(p.Objects) == 0 {
		return ErrPublicationProjection
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if validateGraph(p) != nil {
		return ErrPublicationProjection
	}
	// Use the existing canonical parser and graph identity checks to map the
	// projected scoped delta onto the full uploaded tree, including base blobs.
	byID := map[string]GitObject{}
	for _, o := range p.Objects {
		byID[o.OID] = o
	}
	files := map[string]treeEntry{}
	visits := 0
	var walk func(string, string, int) error
	walk = func(oid, prefix string, depth int) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		visits++
		if depth > 32 || visits > maxObjects || len(files) > maxObjects {
			return ErrPublicationProjection
		}
		entries, err := treeEntries(byID[oid].Raw)
		if err != nil {
			return ErrPublicationProjection
		}
		for _, entry := range entries {
			name := prefix + entry.Path
			if !publicationPath(name) {
				return ErrPublicationContent
			}
			if entry.Type == "tree" {
				if err := walk(entry.SHA, name+"/", depth+1); err != nil {
					return err
				}
			} else {
				files[name] = entry
			}
		}
		return nil
	}
	if err := walk(p.Commit.Tree, "", 0); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, f := range p.Commit.Files {
		entry, exists := files[f.Path]
		if seen[f.Path] || f.Mode == "000000" && exists || f.Mode != "000000" && (!exists || entry.Mode != f.Mode || entry.SHA != objectOID("blob", f.Bytes)) {
			return ErrPublicationProjection
		}
		seen[f.Path] = true
	}
	authored, e := authoredObject(p.Commit)
	if e != nil || authored.OID != p.Head || p.Commit.Commit != p.Head || p.Commit.Parent != p.BaseHead || p.Commit.Branch != p.Branch || !bytes.Equal(authored.Raw, p.Objects[len(p.Objects)-1].Raw) {
		return ErrPublicationProjection
	}
	budget := maxGraphBytes * 4
	texts := []string{p.PRTitle, p.PRBody, p.Commit.Message, p.Commit.Author.Name, p.Commit.Author.Email, p.Commit.Committer.Name, p.Commit.Committer.Email, p.Actor, p.Repository, p.Base, p.Branch}
	texts = append(texts, p.RequiredChecks...)
	texts = append(texts, p.RequiredReviewers...)
	texts = append(texts, p.PostMergeChecks...)
	for _, s := range texts {
		if e := publicationInspect(ctx, []byte(s), 0, &budget); e != nil {
			return e
		}
	}
	for _, f := range p.Commit.Files {
		if !publicationPath(f.Path) || (f.Mode != "100644" && f.Mode != "100755" && f.Mode != "000000") || f.Mode == "000000" && len(f.Bytes) != 0 {
			return ErrPublicationContent
		}
		if e := publicationInspect(ctx, f.Bytes, 0, &budget); e != nil {
			return e
		}
	}
	for _, o := range p.Objects {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		switch o.Kind {
		case "blob", "commit":
			if e := publicationInspect(ctx, o.Raw, 0, &budget); e != nil {
				return e
			}
		case "tree":
			entries, e := treeEntries(o.Raw)
			if e != nil {
				return ErrPublicationProjection
			}
			for _, entry := range entries {
				if !publicationPath(entry.Path) {
					return ErrPublicationContent
				}
			}
		default:
			return ErrPublicationProjection
		}
	}
	return ctx.Err()
}
