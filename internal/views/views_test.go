package views

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) (*vault.Vault, *index.Index) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "Projekt/A.md", "---\nstatus: draft\nupdated: 2026-11-01\ntags: [projekt/nota]\n---\nA\n")
	write(t, root, "Projekt/B.md", "---\nstatus: draft\nupdated: 2026-11-20\n---\nB #projekt\n")
	write(t, root, "C.md", "---\nstatus: done\npriority: 1\n---\nC\n")
	write(t, root, ".nota/views/drafts.yaml", "name: Drafts\nicon: pen\nwhere: { status: draft }\nsort: updated desc\n")
	write(t, root, ".nota/views/projekt.yml", "name: Projekt\ntag: projekt\nfolder: Projekt\nlimit: 1\nsort: title\n")
	write(t, root, ".nota/views/prio.yaml", "where:\n  priority: 1\n")
	write(t, root, ".nota/views/README.md", "not a view")
	v, err := vault.Open(root, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.OpenVault(v)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ix.Close() })
	if _, err := ix.Sync(v); err != nil {
		t.Fatal(err)
	}
	return v, ix
}

func eval(t *testing.T, ix *index.Index, vw View) []string {
	t.Helper()
	es, err := Eval(ix, vw)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Path)
	}
	return out
}

func TestLoadAndEval(t *testing.T) {
	v, ix := setup(t)
	vs, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, vw := range vs {
		names = append(names, vw.ID+":"+vw.Name)
	}
	if !slices.Equal(names, []string{"drafts:Drafts", "prio:prio", "projekt:Projekt"}) {
		t.Fatalf("views %q", names)
	}
	drafts, ok := Find(vs, "DRAFTS")
	if !ok || drafts.Icon != "pen" || drafts.File != ".nota/views/drafts.yaml" {
		t.Fatalf("find: %+v %v", drafts, ok)
	}
	if got := eval(t, ix, drafts); !slices.Equal(got, []string{"Projekt/B.md", "Projekt/A.md"}) {
		t.Fatalf("drafts %q", got)
	}
	p, _ := Find(vs, "projekt")
	if got := eval(t, ix, p); !slices.Equal(got, []string{"Projekt/A.md"}) {
		t.Fatalf("projekt %q", got)
	}
	prio, _ := Find(vs, "prio")
	if got := eval(t, ix, prio); !slices.Equal(got, []string{"C.md"}) {
		t.Fatalf("prio %q", got)
	}
	if _, ok := Find(vs, "nope"); ok {
		t.Fatal("found missing view")
	}
}

func TestInvalidViews(t *testing.T) {
	for name, src := range map[string]string{
		"bad sort":      "name: X\nsort: sideways\n",
		"unknown field": "name: X\nfilter: y\n",
		"list in where": "name: X\nwhere: { tags: [a, b] }\n",
		"broken yaml":   "name: [x\n",
	} {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	v, _ := setup(t)
	write(t, v.Root(), ".nota/views/zbroken.yaml", "sort: sideways\n")
	if _, err := Load(v); err == nil || !strings.Contains(err.Error(), "zbroken.yaml") {
		t.Fatalf("Load error should name the file: %v", err)
	}
}

func TestNoViewsDir(t *testing.T) {
	v, err := vault.Open(t.TempDir(), vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	vs, err := Load(v)
	if err != nil || len(vs) != 0 {
		t.Fatalf("%v %v", vs, err)
	}
}
