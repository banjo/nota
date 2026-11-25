package frontmatter

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/parser"
)

const sample = `---
# Synced from Obsidian, keep the comment
title:   "Sync-protokoll"
tags:
  - nota
  - sync
status: draft   # inline comment

custom: {a: 1, b: [x, y]}
created: 2026-11-18T10:12:00+01:00
---
# Sync-protokoll

Body with a rule below.

---

More body. tags: not-frontmatter
`

func TestSplit(t *testing.T) {
	d := Split([]byte(sample))
	if !d.Has() {
		t.Fatal("expected frontmatter")
	}
	if !strings.HasPrefix(string(d.Body()), "# Sync-protokoll") {
		t.Fatalf("body starts wrong: %q", d.Body()[:20])
	}
	p, err := d.Properties()
	if err != nil {
		t.Fatal(err)
	}
	if p["status"] != "draft" || p["title"] != "Sync-protokoll" {
		t.Fatalf("properties: %#v", p)
	}
	if !reflect.DeepEqual(p["tags"], []any{"nota", "sync"}) {
		t.Fatalf("tags: %#v", p["tags"])
	}
}

func TestNoFrontmatter(t *testing.T) {
	for _, src := range []string{"# Title\n\nbody\n", "", "---\nunclosed: true\nbody\n", " ---\nx: 1\n---\n"} {
		d := Split([]byte(src))
		if d.Has() {
			t.Errorf("%q: unexpected frontmatter", src)
		}
		if string(d.Body()) != src {
			t.Errorf("%q: body changed", src)
		}
	}
}

// Setting one key must leave every other byte of the file untouched.
func TestSetIsByteStable(t *testing.T) {
	out, err := Set([]byte(sample), "status", "done")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sample, "status: draft   # inline comment\n", "status: done\n", 1)
	if string(out) != want {
		t.Fatalf("diff:\n%s", out)
	}
}

// SetLiteral writes the value as it is, without quoting.
func TestSetLiteral(t *testing.T) {
	out, err := SetLiteral([]byte(sample), "updated", "2026-11-22T10:42:00+01:00")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "\nupdated: 2026-11-22T10:42:00+01:00\n") {
		t.Fatalf("unquoted line missing:\n%s", out)
	}
	if _, err := SetLiteral([]byte(sample), "updated", "a\nb"); err == nil {
		t.Fatal("a newline in the value must be refused")
	}
}

// Dates and timestamps are written without quotes, as Obsidian and the
// created/updated lines write them; other text is encoded as before.
func TestSetDatesUnquoted(t *testing.T) {
	cases := map[string]string{
		"2026-12-24":                    "due: 2026-12-24",
		"2026-12-24T10:00:00+01:00":     "due: 2026-12-24T10:00:00+01:00",
		"2026-12-24T10:00:00.5Z":        "due: 2026-12-24T10:00:00.5Z",
		"2026-13-01":                    "due: 2026-13-01",
		"2026-12-24 10:00":              `due: "2026-12-24 10:00"`,
		"2026-12-24T10:00:00+01:00 ish": `due: "2026-12-24T10:00:00+01:00 ish"`,
	}
	for in, want := range cases {
		out, err := Set([]byte("# Trip\n"), "due", in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if !strings.Contains(string(out), "\n"+want+"\n") {
			t.Errorf("%s: got\n%s", in, out)
		}
		p, err := Split(out).Properties()
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if p["due"] != in {
			t.Errorf("%s: reads back as %#v", in, p["due"])
		}
	}
}

func TestSetReplacesBlockValue(t *testing.T) {
	out, err := Set([]byte(sample), "tags", []string{"nota", "sync", "spike"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sample, "tags:\n  - nota\n  - sync\n", "tags: [nota, sync, spike]\n", 1)
	if string(out) != want {
		t.Fatalf("got:\n%s", out)
	}
}

func TestSetAppendsMissingKey(t *testing.T) {
	out, err := Set([]byte(sample), "aliases", []string{"sync-spec"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(sample, "+01:00\n---\n", "+01:00\naliases: [sync-spec]\n---\n", 1)
	if string(out) != want {
		t.Fatalf("got:\n%s", out)
	}
}

func TestSetCreatesBlock(t *testing.T) {
	out, err := Set([]byte("# Hej\n"), "tags", []string{"inbox"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "---\ntags: [inbox]\n---\n# Hej\n" {
		t.Fatalf("got %q", out)
	}
}

func TestCRLFAndBOM(t *testing.T) {
	src := "\xef\xbb\xbf---\r\ntitle: Möte\r\nstatus: draft\r\n---\r\nBody\r\n"
	out, err := Set([]byte(src), "status", "done")
	if err != nil {
		t.Fatal(err)
	}
	want := "\xef\xbb\xbf---\r\ntitle: Möte\r\nstatus: done\r\n---\r\nBody\r\n"
	if string(out) != want {
		t.Fatalf("got %q", out)
	}
	out, _ = Set([]byte("\xef\xbb\xbfBody\r\n"), "a", 1)
	if string(out) != "\xef\xbb\xbf---\r\na: 1\r\n---\r\nBody\r\n" {
		t.Fatalf("new block with BOM/CRLF: %q", out)
	}
}

func TestDelete(t *testing.T) {
	out := Delete([]byte(sample), "tags")
	want := strings.Replace(sample, "tags:\n  - nota\n  - sync\n", "", 1)
	if string(out) != want {
		t.Fatalf("got:\n%s", out)
	}
	if !bytes.Equal(Delete([]byte(sample), "missing"), []byte(sample)) {
		t.Fatal("deleting a missing key changed the file")
	}
}

// Values that need quoting or contain Swedish characters survive a round trip.
func TestValuesRoundTrip(t *testing.T) {
	values := map[string]any{
		"title":   "Möte: planering #1",
		"quote":   `she said "hej"`,
		"yes":     "yes",
		"num":     "0123",
		"empty":   "",
		"bool":    true,
		"int":     42,
		"list":    []any{"å", "ä", "ö", "a b", "c: d"},
		"date":    "2026-11-22",
		"leading": " space",
	}
	src := []byte(sample)
	for k, v := range values {
		var err error
		if src, err = Set(src, k, v); err != nil {
			t.Fatalf("%s: %v", k, err)
		}
	}
	p, err := Split(src).Properties()
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	for k, v := range values {
		got := p[k]
		if n, ok := got.(uint64); ok {
			got = int(n)
		}
		if !reflect.DeepEqual(got, v) {
			t.Errorf("%s: got %#v (%T), want %#v\n%s", k, got, got, v, src)
		}
	}
	if !bytes.HasSuffix(src, []byte("tags: not-frontmatter\n")) || !bytes.Contains(src, []byte("# Synced from Obsidian, keep the comment\n")) {
		t.Fatal("body or comments changed")
	}
}

// TestASTRoundTripReport documents why edits are line-based: re-serialising
// goccy/go-yaml's AST does not reproduce the original bytes.
func TestASTRoundTripReport(t *testing.T) {
	front := Split([]byte(sample)).Front()
	f, err := parser.ParseBytes(front, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	got := f.String()
	if got == strings.TrimRight(string(front), "\n") || got == string(front) {
		t.Log("goccy AST round trip is byte-identical for the sample")
		return
	}
	t.Logf("goccy AST round trip changes the bytes:\n--- original\n%s--- re-serialised\n%s", front, got)
}

func FuzzSetKeepsBody(f *testing.F) {
	f.Add("body\n", "status", "x")
	f.Add("---\na: 1\n---\n---\nb: 2\n", "a", "y")
	f.Fuzz(func(t *testing.T, body, key, value string) {
		if key == "" || strings.ContainsAny(key, "\r\n") {
			t.Skip()
		}
		src := []byte("---\ntitle: x\n---\n" + body)
		out, err := Set(src, key, value)
		if err != nil {
			return
		}
		if got := Split(out).Body(); string(got) != body {
			t.Fatalf("body changed: %q -> %q", body, got)
		}
	})
}
