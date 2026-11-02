package delivery

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"time"
)

func objectPath(i Intent, o GitObject) string {
	return "/repos/" + i.Repository + "/git/" + o.Kind + "s"
}
func (g *GitHub) readObject(ctx context.Context, s []byte, i Intent, o GitObject) error {
	path := objectPath(i, o) + "/" + o.OID
	switch o.Kind {
	case "blob":
		var r struct{ SHA, Encoding, Content string }
		if e := g.request(ctx, s, "GET", path, nil, &r); e != nil {
			return e
		}
		b, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(r.Content, "\n", ""))
		if e != nil || r.Encoding != "base64" || r.SHA != o.OID || !bytes.Equal(b, o.Raw) {
			return ErrDenied
		}
	case "tree":
		var r struct {
			SHA       string
			Truncated bool
			Tree      []treeEntry
		}
		if e := g.request(ctx, s, "GET", path, nil, &r); e != nil {
			return e
		}
		entries, e := treeEntries(o.Raw)
		if e != nil || r.SHA != o.OID || r.Truncated || !reflect.DeepEqual(entries, r.Tree) {
			return ErrDenied
		}
		var raw []byte
		for _, entry := range r.Tree {
			mode := entry.Mode
			if mode == "040000" {
				mode = "40000"
			}
			raw = append(raw, []byte(mode+" "+entry.Path+"\x00")...)
			oid, e := hex.DecodeString(entry.SHA)
			if e != nil {
				return ErrDenied
			}
			raw = append(raw, oid...)
		}
		if objectOID("tree", raw) != o.OID {
			return ErrDenied
		}
	case "commit":
		var r struct {
			SHA, Message      string
			Tree              struct{ SHA string }
			Parents           []struct{ SHA string }
			Author, Committer gitPerson
			Verification      struct{ Signature *string }
		}
		if e := g.request(ctx, s, "GET", path, nil, &r); e != nil {
			return e
		}
		p, e := commitPayload(o)
		if e != nil || r.SHA != o.OID || r.Message != p.Message || r.Tree.SHA != p.Tree || len(r.Parents) != len(p.Parents) || r.Verification.Signature != nil || !samePerson(r.Author, p.Author) || !samePerson(r.Committer, p.Committer) {
			return ErrDenied
		}
		for n, parent := range r.Parents {
			if parent.SHA != p.Parents[n] {
				return ErrDenied
			}
		}
	default:
		return ErrDenied
	}
	return nil
}
func samePerson(a, b gitPerson) bool {
	at, ae := time.Parse(time.RFC3339, a.Date)
	bt, be := time.Parse(time.RFC3339, b.Date)
	return ae == nil && be == nil && at.Equal(bt) && a.Name == b.Name && a.Email == b.Email
}
func (g *GitHub) objectEffect(ctx context.Context, i Intent, p Plan, guard Guard, write bool) Observation {
	unknown := Observation{Outcome: Unknown, Code: "object_unknown"}
	expected, e := intentFor(p, i.Operation)
	if e != nil || expected != i || guard == nil || !permits(p.Delivery, i.Operation) || g.review(ctx, i, p) != nil {
		return Observation{Outcome: Denied, Code: "object_denied"}
	}
	// Read-side credential is disposed before fresh mutation custody resolution.
	e = g.custody.Use(ctx, i, func(s []byte) error {
		if guard(ctx) != nil {
			return ErrDenied
		}
		if _, e := g.identity(ctx, s, i, write); e != nil {
			return e
		}
		return g.readObject(ctx, s, i, p.Object)
	})
	if e == nil {
		return Observation{Outcome: Confirmed, Head: p.Object.OID}
	}
	if !write || !errors.Is(e, errAbsent) {
		return unknown
	}
	var payload any
	switch p.Object.Kind {
	case "blob":
		payload = struct {
			Content  string `json:"content"`
			Encoding string `json:"encoding"`
		}{base64.StdEncoding.EncodeToString(p.Object.Raw), "base64"}
	case "tree":
		entries, e := treeEntries(p.Object.Raw)
		if e != nil {
			return unknown
		}
		payload = struct {
			Tree []treeEntry `json:"tree"`
		}{entries}
	case "commit":
		v, e := commitPayload(p.Object)
		if e != nil {
			return unknown
		}
		payload = v
	default:
		return unknown
	}
	// Each dependency must be present; no arbitrary/unapproved object references.
	e = g.custody.Use(ctx, i, func(s []byte) error {
		for _, o := range p.Objects {
			if o.OID == p.Object.OID {
				break
			}
			if e := g.readObject(ctx, s, i, o); e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		return unknown
	}
	if guard(ctx) != nil {
		return unknown
	}
	e = g.custody.Use(ctx, i, func(s []byte) error {
		if _, e := g.identity(ctx, s, i, true); e != nil {
			return e
		}
		if p.Object.Kind == "commit" {
			var parent struct{ SHA string }
			if g.request(ctx, s, "GET", "/repos/"+i.Repository+"/git/commits/"+p.BaseHead, nil, &parent) != nil || parent.SHA != p.BaseHead {
				return ErrDenied
			}
		}
		return nil
	})
	if e != nil || guard(ctx) != nil {
		return unknown
	}
	e = g.custody.Use(ctx, i, func(s []byte) error {
		var r struct{ SHA string }
		// Check the actor using the exact credential surrounding this write,
		// not an earlier separately resolved custody callback.
		if _, e := g.identity(ctx, s, i, true); e != nil {
			return e
		}
		if guard(ctx) != nil {
			return ErrDenied
		}
		if e := g.request(ctx, s, "POST", objectPath(i, p.Object), payload, &r); e != nil {
			return e
		}
		if r.SHA != p.Object.OID {
			return ErrDenied
		}
		return nil
	})
	if e != nil {
		return unknown
	}
	e = g.custody.Use(ctx, i, func(s []byte) error { return g.readObject(ctx, s, i, p.Object) })
	if e != nil {
		return unknown
	}
	return Observation{Outcome: Confirmed, Head: p.Object.OID}
}
func (g *GitHub) graphPresent(ctx context.Context, s []byte, i Intent, p Plan) error {
	if validateGraph(p) != nil {
		return ErrDenied
	}
	for _, o := range p.Objects {
		if e := g.readObject(ctx, s, i, o); e != nil {
			return e
		}
	}
	return nil
}
