package relink

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
)

// setup opens a vault on files, with an index kept current through the
// event bus, as the CLI and the app do.
func setup(t *testing.T, files map[string]string) (*vault.Vault, *index.Index) {
	t.Helper()
	bus := events.New()
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	v, err := vault.OpenFS(vault.NewMemFS(files), "mem", vault.Options{Bus: bus, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Sync(v); err != nil {
		t.Fatal(err)
	}
	bus.Subscribe(func(e events.Event) {
		var err error
		switch e.Type {
		case events.NoteMoved:
			if err = ix.Remove(e.OldPath); err == nil {
				err = ix.Update(v, e.Path)
			}
		case events.NoteDeleted:
			err = ix.Remove(e.Path)
		default:
			err = ix.Update(v, e.Path)
		}
		if err != nil {
			t.Errorf("index: %v", err)
		}
	})
	return v, ix
}

func rename(t *testing.T, v *vault.Vault, ix *index.Index, from, to string) Result {
	t.Helper()
	p, err := Prepare(v, ix, map[string]string{from: to})
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Rename(from, to)
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Apply(map[string]string{from: got})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func body(t *testing.T, v *vault.Vault, p string) string {
	t.Helper()
	n, err := v.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(n.Raw)
}

func TestRenameRewritesEveryKindOfLink(t *testing.T) {
	v, ix := setup(t, map[string]string{
		"Projects/Plan.md": "# Plan\n\nSee [[#Goal]] and [[Plan#Goal]].\n\n## Goal\n",
		"A.md": "---\ntags: [x]\n---\n[[Plan]] [[Plan#Goal|the plan]] ![[Plan]] [[Projects/Plan]] [[plan.md]]\n" +
			"[p](Projects/Plan.md#Goal) [q](<Projects/Plan.md>) [r](/Projects/Plan.md) `[[Plan]]`\n" +
			"| [[Plan\\|table]] |\n\n```\n[[Plan]]\n```\n",
		"B.md": "Nothing here, [[Other]].\n",
	})
	res := rename(t, v, ix, "Projects/Plan.md", "Projects/Road map.md")
	want := "---\ntags: [x]\nupdated: 2026-11-25T12:00:00Z\n---\n[[Road map]] [[Road map#Goal|the plan]] ![[Road map]] [[Projects/Road map]] [[Road map.md]]\n" +
		"[p](Projects/Road%20map.md#Goal) [q](<Projects/Road map.md>) [r](/Projects/Road%20map.md) `[[Plan]]`\n" +
		"| [[Road map\\|table]] |\n\n```\n[[Plan]]\n```\n"
	if got := body(t, v, "A.md"); got != want {
		t.Errorf("A.md =\n%s\nwant\n%s", got, want)
	}
	if got := body(t, v, "Projects/Road map.md"); got != "# Plan\n\nSee [[#Goal]] and [[Road map#Goal]].\n\n## Goal\n" {
		t.Errorf("self link: %q", got)
	}
	if got := body(t, v, "B.md"); got != "Nothing here, [[Other]].\n" {
		t.Errorf("B.md changed: %q", got)
	}
	wantRes := Result{Count: 10, Notes: []Changed{{"A.md", 9}, {"Projects/Road map.md", 1}}}
	if !reflect.DeepEqual(res, wantRes) {
		t.Errorf("result %+v, want %+v", res, wantRes)
	}
	bl, err := ix.Backlinks("Projects/Road map.md")
	if err != nil || len(bl) != 10 {
		t.Errorf("backlinks after: %d %v", len(bl), err)
	}
}

func TestMoveKeepsNamesAndFixesPaths(t *testing.T) {
	v, ix := setup(t, map[string]string{
		"Projects/Plan.md":     "# Plan\n\n[o](Other.md) [[Other]] ![d](img/d.png) [web](https://x.se/a.md)\n",
		"Projects/Other.md":    "# Other\n",
		"Projects/img/d.png":   "png",
		"Inbox/Link.md":        "[[Plan]] [[Projects/Plan|p]] [m](../Projects/Plan.md) [s](./x/../../Projects/Plan.md)\n",
		"Inbox/Alias.md":       "---\naliases: [Planen]\n---\n# Alias\n",
		"Inbox/ByAlias.md":     "[[Planen]] [[Plan]]\n",
		"Archive/Unrelated.md": "[[Other]]\n",
	})
	res := rename(t, v, ix, "Projects/Plan.md", "Archive/2026/Plan.md")
	if got := body(t, v, "Inbox/Link.md"); got != "[[Plan]] [[Archive/2026/Plan|p]] [m](../Archive/2026/Plan.md) [s](../Archive/2026/Plan.md)\n" {
		t.Errorf("Link.md = %q", got)
	}
	if got := body(t, v, "Archive/2026/Plan.md"); got != "# Plan\n\n[o](../../Projects/Other.md) [[Other]] ![d](../../Projects/img/d.png) [web](https://x.se/a.md)\n" {
		t.Errorf("moved note = %q", got)
	}
	if got := body(t, v, "Inbox/ByAlias.md"); got != "[[Planen]] [[Plan]]\n" {
		t.Errorf("ByAlias.md = %q", got)
	}
	if res.Count != 5 || len(res.Notes) != 2 {
		t.Errorf("result %+v", res)
	}
}

func TestMoveDoesNotStealALink(t *testing.T) {
	v, ix := setup(t, map[string]string{
		"A/Plan.md":    "# A plan\n",
		"B/Plan.md":    "# B plan\n",
		"Notes/See.md": "[[Plan]]\n",
	})
	if to, _ := ix.ResolveLink("Plan", "Notes/See.md", true); to != "A/Plan.md" {
		t.Fatalf("before: %q", to)
	}
	rename(t, v, ix, "B/Plan.md", "Notes/Plan.md")
	if got := body(t, v, "Notes/See.md"); got != "[[A/Plan]]\n" {
		t.Errorf("See.md = %q", got)
	}
}

func TestRenameFolder(t *testing.T) {
	v, ix := setup(t, map[string]string{
		"Projects/Plan.md":      "# Plan\n\n[[Deep]] [d](Sub/Deep.md) ![i](img/i.png) [o](../Root.md)\n",
		"Projects/Sub/Deep.md":  "# Deep\n\n[up](../Plan.md)\n",
		"Projects/img/i.png":    "png",
		"Root.md":               "[[Projects/Plan]] [[Sub/Deep]] [[Deep]] ![i](Projects/img/i.png) [p](Projects/Plan.md)\n",
		"Other/Projects/Not.md": "# Not\n",
	})
	p, err := PrepareFolder(v, ix, "Projects", "My Work")
	if err != nil {
		t.Fatal(err)
	}
	to, err := v.RenameFolder("Projects", "My Work")
	if err != nil {
		t.Fatal(err)
	}
	if to != "My Work" {
		t.Fatalf("to %q", to)
	}
	res, err := p.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := body(t, v, "Root.md"); got != "[[My Work/Plan]] [[Sub/Deep]] [[Deep]] ![i](My%20Work/img/i.png) [p](My%20Work/Plan.md)\n" {
		t.Errorf("Root.md = %q", got)
	}
	if got := body(t, v, "My Work/Plan.md"); got != "# Plan\n\n[[Deep]] [d](Sub/Deep.md) ![i](img/i.png) [o](../Root.md)\n" {
		t.Errorf("Plan.md = %q", got)
	}
	if res.Count != 3 || len(res.Notes) != 1 {
		t.Errorf("result %+v", res)
	}
}

func TestChangedNoteIsLeftAlone(t *testing.T) {
	v, ix := setup(t, map[string]string{"Plan.md": "# Plan\n", "Link.md": "[[Plan]]\n"})
	p, err := Prepare(v, ix, map[string]string{"Plan.md": "Road.md"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Write("Link.md", []byte("[[Plan]] edited\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Rename("Plan.md", "Road.md"); err != nil {
		t.Fatal(err)
	}
	res, err := p.Apply(nil)
	if err != nil || res.Count != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestRelative(t *testing.T) {
	for _, c := range []struct{ dir, to, want string }{
		{"", "a/b.md", "a/b.md"},
		{"a", "a/b.md", "b.md"},
		{"a/x", "a/b.md", "../b.md"},
		{"x/y", "a/b.md", "../../a/b.md"},
		{"a", "a/c/b.md", "c/b.md"},
	} {
		if got := relative(c.dir, c.to); got != c.want {
			t.Errorf("relative(%q, %q) = %q, want %q", c.dir, c.to, got, c.want)
		}
	}
}
