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
