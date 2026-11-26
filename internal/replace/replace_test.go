package replace

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/protocol"
	"github.com/banjo/nota/internal/vault"
)

const note = "---\ntitle: Plan for planen\ntags: [plan]\n---\n# Plan\n\nPlanen är klar. Se `planen` och planera.\n\n```\nplanen i kod\n```\nSista planen_x och planen.\n"

func texts(ms []Match) []string {
	out := []string{}
	for _, m := range ms {
		out = append(out, m.Text)
	}
	return out
}

func TestMatches(t *testing.T) {
	cases := []struct {
		name string
		o    Options
		want []string
	}{
		{"body only, code skipped, any case", Options{Find: "planen"}, []string{"Planen", "planen", "planen"}},
		{"case", Options{Find: "planen", Case: true}, []string{"planen", "planen"}},
		{"whole word (Unicode-aware, _ is a word character)", Options{Find: "planen", Word: true}, []string{"Planen", "planen"}},
		{"code included", Options{Find: "planen", Code: true}, []string{"Planen", "planen", "planen", "planen", "planen"}},
		{"regex", Options{Find: `plan(\w*)`, Regex: true, Case: true}, []string{"planera", "planen_x", "planen"}},
	}
	for _, c := range cases {
		r, err := New(c.o)
		if err != nil {
			t.Fatal(err)
		}
		if got := texts(r.Matches([]byte(note))); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestApply(t *testing.T) {
	r, _ := New(Options{Find: `plan(en)?\b`, Replace: "förslag$1", Regex: true})
	ms := r.Matches([]byte(note))
	if m := ms[1]; m.Line != 7 || m.Before != "" || m.After != " är klar. Se `planen` och planera." || m.With != "förslagen" {
		t.Fatalf("second match %+v", m)
	}
	got := string(Apply([]byte(note), ms))
	want := "---\ntitle: Plan for planen\ntags: [plan]\n---\n# förslag\n\nförslagen är klar. Se `planen` och planera.\n\n```\nplanen i kod\n```\nSista planen_x och förslagen.\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	// Only some: the literal replacement keeps $1 as it is.
	r, _ = New(Options{Find: "planen", Replace: "$1"})
	ms = r.Matches([]byte(note))
	if got := string(Apply([]byte(note), ms[2:])); got != note[:len(note)-8]+"$1.\n" {
		t.Fatalf("picked %q", got)
	}
}

func TestSkipsLinkTargets(t *testing.T) {
	r, _ := New(Options{Find: "plan"})
	body := "[[Plan#Plan|plan]] [plan](Plan.md) [[Projekt/Plan]] https://x.se/plan\n"
	got := []int{}
	for _, m := range r.Matches([]byte(body)) {
		got = append(got, m.Start)
	}
	if want := []int{12, 20, 65}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestGroupsAsInJavaScript(t *testing.T) {
	r, _ := New(Options{Find: `(kund|plan)en`, Replace: "$1erna $$1 ${1}s", Regex: true})
	if ms := r.Matches([]byte("kunden\n")); ms[0].With != "kunderna $1 kunds" {
		t.Fatalf("with %q", ms[0].With)
	}
}

func TestNewRejects(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("empty find")
	}
	if _, err := New(Options{Find: "(a", Regex: true}); err == nil || err.Error() != "invalid regular expression: missing closing ): `(a`" {
		t.Fatalf("bad regex: %v", err)
	}
}

func TestFindAndApplyNotes(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"A.md": note, "Sub/B.md": "Planen igen\n", "C.md": "Inget här\n"}
	for p, c := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, p), []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	v, err := vault.Open(dir, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.OpenVault(v)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.Sync(v); err != nil {
		t.Fatal(err)
	}
	r, _ := New(Options{Find: "planen", Replace: "förslaget", Word: true})
	ns, err := Find(v, ix, r, index.Query{Sort: index.Sort{Field: "path"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 2 || ns[0].Path != "A.md" || len(ns[0].Matches) != 2 || ns[1].Path != "Sub/B.md" {
		t.Fatalf("find %+v", ns)
	}
	// B changed since: it is skipped and reported, A is written.
	if err := os.WriteFile(filepath.Join(dir, "Sub/B.md"), []byte("Planen igen, ändrad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ApplyNotes(v, r, []Pick{{Path: "A.md", Hash: ns[0].Hash, Matches: []int{1}}, {Path: "Sub/B.md", Hash: ns[1].Hash}})
	if err != nil || !slices.Equal(res.Stale, []string{"Sub/B.md"}) {
		t.Fatalf("stale: %v %v", res.Stale, err)
	}
	if res.Count != 1 || len(res.Notes) != 1 || res.Notes[0].Path != "A.md" {
		t.Fatalf("result %+v", res)
	}
	a, _ := v.Read("A.md")
	if res.Notes[0].Hash != protocol.Hash(a.Raw) || !slices.Contains(texts(r.Matches(a.Raw)), "Planen") {
		t.Fatalf("A after: %s", a.Raw)
	}
	if string(a.Body) == res.Notes[0].Before {
		t.Fatal("before should be the old body")
	}
}
