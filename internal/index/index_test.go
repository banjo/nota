package index

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSearch(t *testing.T) {
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	notes := [][3]string{
		{"Projekt/Nota/Sync-protokoll.md", "Sync-protokoll", "Filnivå-sync med 3-way merge"},
		{"Fotboll/Junior.md", "Junior: träning tisdag", "Rondo och omställning"},
		{"Hem/Möte.md", "Möte med förskolan", "Frågor om sömn"},
	}
	for _, n := range notes {
		if err := ix.Put(n[0], n[1], n[2]); err != nil {
			t.Fatal(err)
		}
	}
	for q, want := range map[string][]string{
		"merge":        {"Projekt/Nota/Sync-protokoll.md"},
		"träning":      {"Fotboll/Junior.md"},
		"möte":         {"Hem/Möte.md"},
		"förskol*":     {"Hem/Möte.md"},
		"sync OR sömn": {"Projekt/Nota/Sync-protokoll.md", "Hem/Möte.md"},
	} {
		got, err := ix.Search(q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", q, got, want)
		}
	}
}

// TestSearchBudget checks the 50 ms search budget on a 10,000-note index.
func TestSearchBudget(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	ix, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	tx, _ := ix.db.Begin()
	start := time.Now()
	for i := range 10000 {
		body := fmt.Sprintf("Anteckning %d om fotboll, recept och kod. Ord%d unik%d.", i, i%97, i)
		if _, err := tx.Exec(`INSERT INTO notes(path, title, body) VALUES (?, ?, ?)`, fmt.Sprintf("n/%d.md", i), fmt.Sprintf("Note %d", i), body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Logf("indexed 10,000 notes in %v", time.Since(start))
	for _, q := range []string{"unik9999", "ord42", "fotboll"} {
		start := time.Now()
		res, err := ix.Search(q)
		if err != nil {
			t.Fatal(err)
		}
		d := time.Since(start)
		t.Logf("search %q: %d hits in %v", q, len(res), d)
		if q != "fotboll" && d > 50*time.Millisecond {
			t.Errorf("search %q took %v, budget 50ms", q, d)
		}
	}
}
