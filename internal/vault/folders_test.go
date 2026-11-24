package vault

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// folderVaults runs a test on a real directory and on a MemFS.
func folderVaults(t *testing.T, fn func(t *testing.T, v *Vault)) {
	t.Run("dir", func(t *testing.T) { fn(t, newVault(t)) })
	t.Run("mem", func(t *testing.T) {
		v, err := OpenFS(NewMemFS(nil), "mem", Options{})
		if err != nil {
			t.Fatal(err)
		}
		fn(t, v)
	})
}

func folders(t *testing.T, v *Vault) []string {
	t.Helper()
	fs, err := v.Folders()
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestMkdirNestedAndFolders(t *testing.T) {
	folderVaults(t, func(t *testing.T, v *Vault) {
		if _, _, err := v.Write("Notes/a.md", []byte("# A")); err != nil {
			t.Fatal(err)
		}
		got, err := v.Mkdir(`Projects\Ideas/Q1`)
		if err != nil || got != "Projects/Ideas/Q1" {
			t.Fatalf("Mkdir = %q, %v", got, err)
		}
		if _, err := v.Mkdir(nfd); err != nil {
			t.Fatal(err)
		}
		want := []string{"Möte", "Notes", "Projects", "Projects/Ideas", "Projects/Ideas/Q1"}
		if got := folders(t, v); !slices.Equal(got, want) {
			t.Fatalf("Folders = %q, want %q", got, want)
		}
		// Empty folders hold no notes.
		if ps := paths(t, v); !slices.Equal(ps, []string{"Notes/a.md"}) {
			t.Fatalf("notes = %q", ps)
		}
	})
}

func TestMkdirErrors(t *testing.T) {
	folderVaults(t, func(t *testing.T, v *Vault) {
		if _, _, err := v.Write("Notes/a.md", []byte("# A")); err != nil {
			t.Fatal(err)
		}
		if _, err := v.Mkdir("Work"); err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			name, in string
			want     error
		}{
			{"exists", "Work", ErrExists},
			{"exists with notes", "Notes", ErrExists},
			{"exists NFD", "Work/", ErrExists},
			{"case collision", "work", ErrCaseCollision},
			{"case collision in parent", "NOTES/new", ErrCaseCollision},
			{"file in the way", "Notes/a.md/x", ErrInvalidPath},
			{"root", "", ErrInvalidPath},
			{"root dot", ".", ErrInvalidPath},
			{"outside", "../x", ErrInvalidPath},
			{"meta", ".nota", ErrInvalidPath},
			{"meta child", ".nota/views/x", ErrInvalidPath},
			{"trash", ".trash/x", ErrInvalidPath},
			{"hidden", "a/.secret", ErrInvalidPath},
			{"reserved", "CON", ErrInvalidPath},
			{"reserved nested", "a/nul", ErrInvalidPath},
			{"colon", "a:b", ErrInvalidPath},
			{"trailing dot", "a.", ErrInvalidPath},
			{"blank", "a/ /b", ErrInvalidPath},
			{"note name", "todo.md", ErrInvalidPath},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				_, err := v.Mkdir(c.in)
				if !errors.Is(err, c.want) {
					t.Fatalf("Mkdir(%q) = %v, want %v", c.in, err, c.want)
				}
			})
		}
		if got := folders(t, v); !slices.Equal(got, []string{"Notes", "Work"}) {
			t.Fatalf("a refused Mkdir left folders behind: %q", got)
		}
	})
}

func TestMkdirIgnoredByNotaignore(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, IgnoreFile, "drafts/\n")
	if err := v.ReloadIgnore(); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Mkdir("drafts"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Mkdir(drafts) = %v", err)
	}
	if _, err := v.Mkdir("a/drafts"); !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("Mkdir(a/drafts) = %v", err)
	}
}

func TestFoldersSkipsHiddenAndIgnored(t *testing.T) {
	v := newVault(t)
	writeFile(t, v, IgnoreFile, "build/\n")
	for _, d := range []string{".git/objects", ".nota", ".trash/x", "build/out", "ok/sub", ".hidden/inner", "node/x"} {
		if err := os.MkdirAll(filepath.Join(v.Root(), d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.ReloadIgnore(); err != nil {
		t.Fatal(err)
	}
	want := []string{"node", "node/x", "ok", "ok/sub"}
	if got := folders(t, v); !slices.Equal(got, want) {
		t.Fatalf("Folders = %q, want %q", got, want)
	}
}

func TestFoldersNFC(t *testing.T) {
	v := newVault(t)
	if err := os.MkdirAll(filepath.Join(v.Root(), nfd), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := folders(t, v); !slices.Equal(got, []string{"Möte"}) {
		t.Fatalf("Folders = %q", got)
	}
	// NFC input finds the NFD directory on disk.
	if _, err := v.Mkdir("Möte"); !errors.Is(err, ErrExists) {
		t.Fatalf("Mkdir = %v, want exists", err)
	}
}

func TestMkdirThenWrite(t *testing.T) {
	folderVaults(t, func(t *testing.T, v *Vault) {
		if _, err := v.Mkdir("Ideas"); err != nil {
			t.Fatal(err)
		}
		n, err := v.Create("Ideas", []byte("# Spark"))
		if err != nil || n.Path != "Ideas/Spark.md" {
			t.Fatalf("Create = %v, %v", n, err)
		}
		if got := folders(t, v); !slices.Equal(got, []string{"Ideas"}) {
			t.Fatalf("Folders = %q", got)
		}
	})
}

func TestRenameFolder(t *testing.T) {
	folderVaults(t, func(t *testing.T, v *Vault) {
		for p, c := range map[string]string{
			"Work/a.md":         "# A",
			"Work/Deep/b.md":    "# B",
			"Work/Deep/pic.png": "png",
			"Other/c.md":        "# C",
		} {
			if _, _, err := v.write(p, []byte(c)); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := v.Mkdir("Work/Empty"); err != nil {
			t.Fatal(err)
		}
		got, err := v.RenameFolder("Work", "Projects")
		if err != nil || got != "Projects" {
			t.Fatalf("RenameFolder = %q, %v", got, err)
		}
		want := []string{"Other", "Projects", "Projects/Deep", "Projects/Empty"}
		if fs := folders(t, v); !slices.Equal(fs, want) {
			t.Fatalf("Folders = %q, want %q", fs, want)
		}
		if ps := paths(t, v); !slices.Equal(ps, []string{"Other/c.md", "Projects/Deep/b.md", "Projects/a.md"}) {
			t.Fatalf("notes = %q", ps)
		}
		if !v.FileExists("Projects/Deep/pic.png") || v.FileExists("Work/Deep/pic.png") {
			t.Fatal("attachment did not move")
		}
		// Move under another folder, nested target.
		if got, err := v.RenameFolder("Projects/Deep", "Other/Inner/Deep"); err != nil || got != "Other/Inner/Deep" {
			t.Fatalf("nested = %q, %v", got, err)
		}
		if ps := paths(t, v); !slices.Contains(ps, "Other/Inner/Deep/b.md") {
			t.Fatalf("notes = %q", ps)
		}
	})
}

func TestRenameFolderErrors(t *testing.T) {
	folderVaults(t, func(t *testing.T, v *Vault) {
		for _, p := range []string{"A/a.md", "B/b.md"} {
			if _, _, err := v.Write(p, []byte("# x")); err != nil {
				t.Fatal(err)
			}
		}
		cases := []struct {
			name, from, to string
			want           error
		}{
			{"missing", "Nope", "X", ErrNotFound},
			{"exists", "A", "B", ErrExists},
			{"case collision", "A", "b", ErrCaseCollision},
			{"case only", "A", "a", ErrCaseCollision},
			{"into itself", "A", "A/Sub", ErrInvalidPath},
			{"root", "", "X", ErrInvalidPath},
			{"hidden", "A", ".x", ErrInvalidPath},
			{"reserved", "A", "CON", ErrInvalidPath},
			{"note name", "A", "x.md", ErrInvalidPath},
			{"file in the way", "A", "A.md/x", ErrInvalidPath},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				_, err := v.RenameFolder(c.from, c.to)
				if !errors.Is(err, c.want) {
					t.Fatalf("RenameFolder(%q, %q) = %v, want %v", c.from, c.to, err, c.want)
				}
			})
		}
		if ps := paths(t, v); !slices.Equal(ps, []string{"A/a.md", "B/b.md"}) {
			t.Fatalf("a refused rename moved notes: %q", ps)
		}
	})
}
