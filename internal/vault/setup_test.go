package vault

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// names.json is shared with the frontend, which checks names as the user
// types (packages/app/tests/setup.test.ts): both must give the same answers.
func TestCheckName(t *testing.T) {
	b, err := os.ReadFile("testdata/names.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		In    string `json:"in"`
		Kind  string `json:"kind"`
		Name  string `json:"name"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("only %d cases", len(cases))
	}
	for _, tt := range cases {
		kind := VaultName
		if tt.Kind == "inbox" {
			kind = InboxName
		}
		got, err := CheckName(tt.In, kind)
		if tt.Error != "" {
			if err == nil || err.Error() != tt.Error || !errors.Is(err, ErrName) {
				t.Errorf("CheckName(%q) = %q, %v; want error %q", tt.In, got, err, tt.Error)
			}
			continue
		}
		if err != nil || got != tt.Name {
			t.Errorf("CheckName(%q) = %q, %v; want %q", tt.In, got, err, tt.Name)
		}
	}
}

func TestCheckLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	file := filepath.Join(home, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		in, want, msg string
	}{
		{"~/Documents", filepath.Join(home, "Documents"), ""},
		{home + "/a/../b/", filepath.Join(home, "b"), ""},
		{"", "", "Choose where the vault should be created."},
		{"Documents", "", "Use a full path, starting with ~ or /."},
		{"/", "", "Nota can’t write to “/”. Pick a folder inside your home folder."},
		{"/usr/local", "", "Nota can’t write to “/usr/local”. Pick a folder inside your home folder."},
		{"/System", "", "Nota can’t write to “/System”. Pick a folder inside your home folder."},
		{file, "", "“" + file + "” is a file, not a folder."},
	}
	for _, tt := range tests {
		got, err := CheckLocation(tt.in)
		if tt.msg != "" {
			if err == nil || err.Error() != tt.msg || !errors.Is(err, ErrLocation) {
				t.Errorf("CheckLocation(%q) = %q, %v; want %q", tt.in, got, err, tt.msg)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("CheckLocation(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
}

func mkfiles(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if strings.HasSuffix(f, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInspectFolder(t *testing.T) {
	root := t.TempDir()
	mkfiles(t, root,
		"empty/",
		"mixed/a.md", "mixed/b.MD", "mixed/sub/c.md", "mixed/pic.png", "mixed/.hidden.md",
		"mixed/.git/x.md", "mixed/.nota/index.db", "mixed/node_modules/p/readme.md",
		"onlymeta/.nota/index.db",
		"afile")
	tests := []struct {
		name string
		path string
		want FolderInfo
	}{
		{"missing", "nope", FolderInfo{}},
		{"empty", "empty", FolderInfo{Exists: true, IsDir: true, Empty: true, Writable: true}},
		{"notes and others", "mixed", FolderInfo{Exists: true, IsDir: true, Notes: 3, Files: 1, Writable: true}},
		{"an earlier vault, no notes", "onlymeta", FolderInfo{Exists: true, IsDir: true, Writable: true}},
		{"a file", "afile", FolderInfo{Exists: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(root, tt.path)
			tt.want.Path = p
			got, err := InspectFolder(p)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
	t.Run("changes nothing", func(t *testing.T) {
		if _, err := InspectFolder(filepath.Join(root, "mixed")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "empty", MetaDirName)); err == nil {
			t.Error(".nota left behind in an inspected folder")
		}
		if _, err := InspectFolder(filepath.Join(root, "empty")); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, "empty", MetaDirName)); err == nil {
			t.Error(".nota left behind in an inspected folder")
		}
	})
	t.Run("relative path", func(t *testing.T) {
		if _, err := InspectFolder("relative/dir"); !errors.Is(err, ErrLocation) {
			t.Errorf("err = %v, want ErrLocation", err)
		}
	})
}

func TestInspectFolderUnwritable(t *testing.T) {
	if os.Getuid() == 0 || os.PathSeparator == '\\' {
		t.Skip("permissions are not enforced for root or on Windows")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	mkfiles(t, dir, "a.md")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	got, err := InspectFolder(dir)
	if err != nil || got.Writable || got.Notes != 1 {
		t.Errorf("got %+v, %v; want 1 note, not writable", got, err)
	}
	if _, err := CreateVaultDir(dir, "Notes"); err == nil || !strings.Contains(err.Error(), "can’t create") {
		t.Errorf("CreateVaultDir in a read-only folder: %v", err)
	}
}

func TestCreateVaultDir(t *testing.T) {
	root := t.TempDir()
	mkfiles(t, root, "taken/a.md", "blank/")
	t.Run("new, with the missing location", func(t *testing.T) {
		got, err := CreateVaultDir(filepath.Join(root, "docs", "deeper"), " My Notes ")
		want := filepath.Join(root, "docs", "deeper", "My Notes")
		if err != nil || got != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
		if fi, err := os.Stat(want); err != nil || !fi.IsDir() {
			t.Fatalf("vault dir not made: %v", err)
		}
		if _, err := os.Stat(filepath.Join(want, MetaDirName)); err == nil {
			t.Error("the writable check left .nota behind")
		}
	})
	t.Run("an empty folder is used as it is", func(t *testing.T) {
		got, err := CreateVaultDir(root, "blank")
		if err != nil || got != filepath.Join(root, "blank") {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("a folder with files is refused and untouched", func(t *testing.T) {
		_, err := CreateVaultDir(root, "taken")
		if !errors.Is(err, ErrNotEmpty) || !strings.Contains(err.Error(), "“taken” already exists") {
			t.Fatalf("err = %v, want ErrNotEmpty", err)
		}
		es, _ := os.ReadDir(filepath.Join(root, "taken"))
		if len(es) != 1 {
			t.Errorf("folder changed: %v", es)
		}
	})
	t.Run("bad names and locations", func(t *testing.T) {
		for _, name := range []string{"", "a/b", ".x", "CON", "a:b"} {
			if _, err := CreateVaultDir(root, name); !errors.Is(err, ErrName) {
				t.Errorf("name %q: err = %v, want ErrName", name, err)
			}
		}
		for _, loc := range []string{"", "relative", "/"} {
			if _, err := CreateVaultDir(loc, "Notes"); !errors.Is(err, ErrLocation) {
				t.Errorf("location %q: err = %v, want ErrLocation", loc, err)
			}
		}
		if es, _ := os.ReadDir(root); len(es) != 3 { // taken, blank and docs, made by the first subtest
			t.Errorf("a refused call left files: %v", es)
		}
	})
}
