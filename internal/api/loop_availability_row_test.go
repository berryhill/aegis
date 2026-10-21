package api

import (
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// Parse only the actual collection row, not a detail form or reconstructed CAS.
// Also reject invalid nesting before the HTML parser can silently repair it.
func renderedLoopRowForm(t *testing.T, body, id, key, button string) url.Values {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	attr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}
	var list *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if attr(n, "id") == "surface-list" {
			list = n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if list == nil {
		t.Fatal("missing rendered collection")
	}
	var row *html.Node
	var locate func(*html.Node)
	locate = func(n *html.Node) {
		if n.Data == "a" && attr(n, "data-record-key") == key {
			row = n.Parent
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			locate(c)
		}
	}
	locate(list)
	if row == nil || row.Data != "li" {
		t.Fatal("missing native row link")
	}
	values := url.Values{}
	forms, buttons := 0, 0
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inside bool) {
		if n.Data == "form" {
			if inside || n.Parent != row || attr(n, "method") != "post" || attr(n, "action") != "/console/loops/availability?loop_id="+url.QueryEscape(id) {
				t.Fatal("availability form must be a direct row sibling of native detail link")
			}
			forms++
			inside = true
		}
		if inside && n.Data == "input" {
			values.Add(attr(n, "name"), attr(n, "value"))
		}
		if inside && n.Data == "button" {
			if n.FirstChild == nil || n.FirstChild.Data != button {
				t.Fatal("wrong row action")
			}
			buttons++
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inside)
		}
	}
	walk(row, false)
	if forms != 1 || buttons != 1 || len(values) != 7 {
		t.Fatalf("row forms=%d buttons=%d fields=%d", forms, buttons, len(values))
	}
	for _, key := range []string{"principal_id", "agent_id", "owner_id", "authority", "publisher"} {
		if values.Has(key) {
			t.Fatalf("browser identity field %s", key)
		}
	}
	return values
}
