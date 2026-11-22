package index

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/banjo/nota/internal/vault"
)

// Performance budgets from the plan (issue #1, decision 11).
const (
	budgetNotes   = 10_000
	budgetReindex = 10 * time.Second
	budgetSearch  = 50 * time.Millisecond
)

var words = strings.Fields(`möte anteckning projekt plan idé sommar vinter kaffe
	resa bok film musik arbete hem familj vän stad skog sjö fjäll båt cykel tåg
	sync index search editor vault markdown frontmatter tag link backlink inbox
	server client protocol merge conflict version history device token phone
	desktop window sidebar palette command view folder file path title body
	alpha beta gamma delta epsilon zeta eta theta iota kappa lambda omicron
	äpple päron banan citron apelsin jordgubbe blåbär hallon lingon smultron
	måndag tisdag onsdag torsdag fredag lördag söndag januari februari mars`)

// genVault writes n notes with frontmatter, headings, tags, wikilinks and
// a code block now and then, spread over nested folders.
func genVault(tb testing.TB, dir string, n int) {
	tb.Helper()
	r := rand.New(rand.NewPCG(1, 2))
	folders := []string{"", "Inbox", "Projekt", "Projekt/Nota", "Projekt/Arkiv", "Möten", "Resor", "Läsning", "Dagbok/2025", "Dagbok/2026"}
	for _, f := range folders {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(f)), 0o755); err != nil {
			tb.Fatal(err)
		}
	}
	word := func() string { return words[r.IntN(len(words))] }
	var b strings.Builder
	for i := range n {
		b.Reset()
		if i%3 != 0 {
			fmt.Fprintf(&b, "---\ntags: [%s, %s/%s]\nstatus: %s\npriority: %d\n---\n",
				word(), word(), word(), []string{"draft", "done", "idea"}[i%3], i%5)
		}
		fmt.Fprintf(&b, "# Note %d %s %s\n\n", i, word(), word())
		for p := range 3 + r.IntN(5) {
			for range 30 + r.IntN(30) {
				b.WriteString(word())
				b.WriteByte(' ')
			}
			if p%2 == 0 {
				fmt.Fprintf(&b, "[[Note %d]] #%s ", r.IntN(n), word())
			}
			b.WriteString("\n\n")
		}
		if i%10 == 0 {
			b.WriteString("```go\n#define [[not a link]]\n```\n")
		}
		if i == 4242 {
			b.WriteString("zyxwvut unique marker\n")
		}
		p := filepath.Join(dir, filepath.FromSlash(folders[i%len(folders)]), fmt.Sprintf("Note %d.md", i))
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			tb.Fatal(err)
		}
	}
}

func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2]
}

// TestPerformanceBudget10k checks the reindex and search budgets on a
// generated 10,000-note vault and logs the numbers (go test -v -run Budget).
func TestPerformanceBudget10k(t *testing.T) {
	if testing.Short() {
		t.Skip("10k-note fixture skipped in -short mode")
	}
	dir := t.TempDir()
	start := time.Now()
	genVault(t, dir, budgetNotes)
	t.Logf("generated %d notes in %v", budgetNotes, time.Since(start).Round(time.Millisecond))

	v, err := vault.Open(dir, vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ix, err := OpenVault(v)
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()

	st, err := ix.Rebuild(v)
	if err != nil {
		t.Fatal(err)
	}
	if st.Added != budgetNotes {
		t.Fatalf("indexed %d notes, want %d", st.Added, budgetNotes)
	}
	t.Logf("full reindex: %v (budget %v)", st.Duration.Round(time.Millisecond), budgetReindex)
	if st.Duration > budgetReindex {
		t.Errorf("full reindex took %v, budget %v", st.Duration, budgetReindex)
	}
	st, err = ix.Sync(v)
	if err != nil || st.Changed() {
		t.Fatalf("no-op sync: %+v %v", st, err)
	}
	t.Logf("incremental sync, nothing changed: %v", st.Duration.Round(time.Millisecond))

	searches := []string{"zyxwvut", "kaffe", "möte projekt", "sync", "lin", `"server client"`, "äpple -päron"}
	for _, q := range searches {
		var ds []time.Duration
		var hits int
		for range 15 {
			s := time.Now()
			es, err := ix.Search(q, Query{})
			ds = append(ds, time.Since(s))
			if err != nil {
				t.Fatalf("search %q: %v", q, err)
			}
			hits = len(es)
		}
		med, worst := median(ds), slices.Max(ds)
		t.Logf("search %-16q %2d hits  median %v  max %v", q, hits, med.Round(time.Microsecond), worst.Round(time.Microsecond))
		if med > budgetSearch {
			t.Errorf("search %q median %v, budget %v", q, med, budgetSearch)
		}
	}
	if es, _ := ix.Search("zyxwvut", Query{}); len(es) != 1 || !strings.HasSuffix(es[0].Path, "Note 4242.md") {
		t.Errorf("unique marker: %q", paths(es))
	}

	timed := func(name string, f func() (int, error)) {
		s := time.Now()
		n, err := f()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%-28s %5d results in %v", name, n, time.Since(s).Round(time.Microsecond))
	}
	timed("query tag=kaffe (nested)", func() (int, error) {
		es, err := ix.Query(Query{Tag: "kaffe", Sort: Sort{Field: "updated", Desc: true}})
		return len(es), err
	})
	timed("query status=done limit 50", func() (int, error) {
		es, err := ix.Query(Query{Where: []Cond{{Key: "status", Value: "done"}}, Limit: 50})
		return len(es), err
	})
	timed("query folder=Projekt", func() (int, error) {
		es, err := ix.Query(Query{Folder: "Projekt"})
		return len(es), err
	})
	timed("backlinks Note 42", func() (int, error) {
		ls, err := ix.Backlinks("Projekt/Note 42.md")
		return len(ls), err
	})
	timed("lookup prefix \"Note 999\"", func() (int, error) {
		ps, err := ix.Lookup("Note 999")
		return len(ps), err
	})
}

// BenchmarkSearch10k measures one search on the 10k fixture.
func BenchmarkSearch10k(b *testing.B) {
	dir := b.TempDir()
	genVault(b, dir, budgetNotes)
	v, _ := vault.Open(dir, vault.Options{})
	ix, err := OpenVault(v)
	if err != nil {
		b.Fatal(err)
	}
	defer ix.Close()
	if _, err := ix.Rebuild(v); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := ix.Search("möte projekt", Query{}); err != nil {
			b.Fatal(err)
		}
	}
}
