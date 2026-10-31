package delivery

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// authoredObject projects the closed UTF-8 commit-tree profile without writing
// any Git object. Unsupported metadata is denied rather than normalized.
func authoredObject(s CommitSpec) (GitObject, error) {
	deny := func() (GitObject, error) { return GitObject{}, ErrDenied }
	if !sha.MatchString(s.Tree) || !sha.MatchString(s.Parent) || !branchName.MatchString(s.Branch) || !strings.Contains(s.Branch, "/") {
		return deny()
	}
	for _, id := range []Identity{s.Author, s.Committer} {
		if !identityOK(id) || !utf8.ValidString(id.Name+id.Email) {
			return deny()
		}
		// Git strips these bytes at ident boundaries; accepting them would silently
		// change reviewed author/committer metadata.
		for _, v := range []string{id.Name, id.Email} {
			crud := func(b byte) bool { return b <= 32 || strings.ContainsRune(".,:;<>\"\\'", rune(b)) }
			if crud(v[0]) || crud(v[len(v)-1]) {
				return deny()
			}
		}
	}
	stamp, e := time.Parse(time.RFC3339, s.Timestamp)
	if e != nil || stamp.Nanosecond() != 0 || strings.Contains(s.Timestamp, ".") || stamp.Unix() < 0 {
		return deny()
	}
	// Parse accepts some non-canonical offsets/fractional forms. Do not silently
	// normalize them into different reviewed metadata. Zero-offset spellings are
	// explicitly equivalent in Git's canonical +0000 representation.
	canonical := stamp.Format(time.RFC3339)
	zeroSpelling := stamp.Format("2006-01-02T15:04:05")
	if s.Timestamp != canonical && s.Timestamp != zeroSpelling+"+00:00" && s.Timestamp != zeroSpelling+"-00:00" {
		return deny()
	}
	_, offset := stamp.Zone()
	if offset%60 != 0 || offset <= -24*3600 || offset >= 24*3600 {
		return deny()
	}
	if len(s.Message) == 0 || len(s.Message) > 16384 || !utf8.ValidString(s.Message) || strings.ContainsRune(s.Message, 0) || secretBytes([]byte(s.Message)) {
		return deny()
	}
	// commit-tree's stdin path preserves message bytes exactly, including an
	// absent final newline; do not use -m/-F's line-completing semantics.
	message := s.Message
	date := fmt.Sprintf("%d %s", stamp.Unix(), stamp.Format("-0700"))
	parents := "parent " + s.Parent + "\n"
	if s.IntegrationParent != "" {
		if !sha.MatchString(s.IntegrationParent) || s.IntegrationParent == s.Parent {
			return deny()
		}
		parents += "parent " + s.IntegrationParent + "\n"
	}
	raw := []byte(fmt.Sprintf("tree %s\n%sauthor %s <%s> %s\ncommitter %s <%s> %s\n\n%s", s.Tree, parents, s.Author.Name, s.Author.Email, date, s.Committer.Name, s.Committer.Email, date, message))
	o := GitObject{Kind: "commit", OID: objectOID("commit", raw), Raw: raw}
	if _, e = commitPayload(o); e != nil || !objectValid(o) {
		return deny()
	}
	return o, nil
}
