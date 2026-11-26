package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/banjo/nota/internal/events"
)

func clockVault(t *testing.T, now *time.Time) *Vault {
	t.Helper()
	v, err := Open(t.TempDir(), Options{Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestTrashListsDeletesNewestFirst(t *testing.T) {
	now := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	v := clockVault(t, &now)
	writeFile(t, v, "Projekt/Plan.md", "# Plan\n")
	writeFile(t, v, "Plan.md", "# Other plan\n")
	if _, err := v.Delete("Projekt/Plan"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if _, err := v.Delete("Plan"); err != nil {
		t.Fatal(err)
	}
	// A second note with the same name gets " 2" in the trash but keeps
	// where it came from.
	writeFile(t, v, "Plan.md", "# Third\n")
	now = now.Add(time.Hour)
	trash, err := v.Delete("Plan")
	if err != nil {
		t.Fatal(err)
	}
	if trash != ".trash/Plan 2.md" {
		t.Fatalf("trash = %q", trash)
	}
	items, err := v.Trash()
	if err != nil {
		t.Fatal(err)
	}
	want := []TrashItem{
		{Path: ".trash/Plan 2.md", From: "Plan.md", DeletedAt: now, Size: 8},
		{Path: ".trash/Plan.md", From: "Plan.md", DeletedAt: now.Add(-time.Hour), Size: 13},
		{Path: ".trash/Projekt/Plan.md", From: "Projekt/Plan.md", DeletedAt: now.Add(-2 * time.Hour), Size: 7},
	}
	if len(items) != len(want) {
		t.Fatalf("items = %+v", items)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("item %d = %+v, want %+v", i, items[i], want[i])
		}
	}
	if it, err := v.FindTrash("Plan"); err != nil || it.Path != ".trash/Plan.md" {
		t.Errorf("FindTrash by trash name = %+v, %v", it, err)
	}
	if it, err := v.FindTrash("Projekt/Plan.md"); err != nil || it.Path != ".trash/Projekt/Plan.md" {
		t.Errorf("FindTrash by path = %+v, %v", it, err)
	}
	if _, err := v.FindTrash("Nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("FindTrash missing: %v", err)
	}
}

func TestTrashWithoutRecordUsesModTime(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, ".trash/Old/Note.md", "x\n")
	mt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(filepath.Join(v.Root(), ".trash/Old/Note.md"), mt, mt); err != nil {
		t.Fatal(err)
	}
	items, err := v.Trash()
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v, %v", items, err)
	}
	if items[0].From != "Old/Note.md" || !items[0].DeletedAt.Equal(mt) {
		t.Errorf("item = %+v", items[0])
	}
}

func TestRestoreTrash(t *testing.T) {
	v := newVault(t)
	var got []events.Event
	v.bus = events.New()
	v.bus.Subscribe(func(e events.Event) { got = append(got, e) })
	writeFile(t, v, "Projekt/Plan.md", "# Plan\n")
	trash, err := v.Delete("Projekt/Plan")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(v.Root(), "Projekt")); err != nil {
		t.Fatal(err)
	}
	// The old path is taken again: refuse, then restore elsewhere.
	writeFile(t, v, "Projekt/Plan.md", "# New plan\n")
	if _, err := v.RestoreTrash(trash, ""); !errors.Is(err, ErrExists) {
		t.Fatalf("restore over a note: %v", err)
	}
	to, err := v.RestoreTrash(trash, "Arkiv/Plan.md")
	if err != nil || to != "Arkiv/Plan.md" {
		t.Fatalf("restore = %q, %v", to, err)
	}
	if !v.Exists("Arkiv/Plan") {
		t.Error("not restored")
	}
	if _, err := os.Stat(filepath.Join(v.Root(), ".trash/Projekt")); !os.IsNotExist(err) {
		t.Errorf("empty trash folder left: %v", err)
	}
	if items, _ := v.Trash(); len(items) != 0 {
		t.Errorf("trash = %+v", items)
	}
	last := got[len(got)-1]
	if last.Type != events.NoteCreated || last.Path != "Arkiv/Plan.md" {
		t.Errorf("last event = %+v", last)
	}
	if _, err := v.RestoreTrash("Arkiv/Plan.md", ""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("restore outside the trash: %v", err)
	}
}

func TestPurgeAndEmptyTrash(t *testing.T) {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	v := clockVault(t, &now)
	for _, n := range []string{"A", "B", "C"} {
		writeFile(t, v, "F/"+n+".md", n+"\n")
		if _, err := v.Delete("F/" + n); err != nil {
			t.Fatal(err)
		}
		now = now.Add(24 * time.Hour)
	}
	if err := v.PurgeTrash(".trash/F/A.md"); err != nil {
		t.Fatal(err)
	}
	// B was deleted Nov 2, C Nov 3: a cutoff of Nov 3 takes B only.
	n, err := v.EmptyTrash(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC))
	if err != nil || n != 1 {
		t.Fatalf("empty older = %d, %v", n, err)
	}
	items, _ := v.Trash()
	if len(items) != 1 || items[0].Path != ".trash/F/C.md" {
		t.Fatalf("left = %+v", items)
	}
	if n, err := v.EmptyTrash(time.Time{}); err != nil || n != 1 {
		t.Fatalf("empty all = %d, %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(v.Root(), ".trash/F")); !os.IsNotExist(err) {
		t.Errorf("trash folder left: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(v.Root(), ".nota/trash.json"))
	if string(data) != "{}\n" {
		t.Errorf("trash log = %q", data)
	}
}
