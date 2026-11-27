package vault

import (
	"errors"
	"testing"
)

func TestFolderIcons(t *testing.T) {
	folderVaults(t, func(t *testing.T, v *Vault) {
		for _, p := range []string{"Work/a.md", "Work/Deep/b.md", "Other/c.md"} {
			if _, _, err := v.Write(p, []byte("# x")); err != nil {
				t.Fatal(err)
			}
		}
		if all, err := v.FolderIcons(); err != nil || len(all) != 0 {
			t.Fatalf("no file: %v, %v", all, err)
		}
		for p, fi := range map[string]FolderIcon{
			"Work":      {Icon: "briefcase", Color: "blue"},
			"Work/Deep": {Icon: "🏠"},
			"Other":     {Color: "red"},
		} {
			if _, err := v.SetFolderIcon(p, fi); err != nil {
				t.Fatalf("SetFolderIcon(%s): %v", p, err)
			}
		}
		data, err := v.ReadFile(FolderIconsFile)
		if err != nil {
			t.Fatal(err)
		}
		want := "Other:\n  color: red\nWork:\n  icon: lucide:briefcase\n  color: blue\nWork/Deep:\n  icon: 🏠\n"
		if string(data) != want {
			t.Errorf("file:\n%s\nwant:\n%s", data, want)
		}

		if _, err := v.RenameFolder("Work", "Projects"); err != nil {
			t.Fatal(err)
		}
		all, err := v.FolderIcons()
		if err != nil {
			t.Fatal(err)
		}
		if all["Projects"].Icon != "lucide:briefcase" || all["Projects/Deep"].Icon != "🏠" || len(all) != 3 {
			t.Errorf("after rename: %v", all)
		}

		if _, err := v.SetFolderIcon("Other", FolderIcon{}); err != nil {
			t.Fatal(err)
		}
		if all, _ := v.FolderIcons(); len(all) != 2 {
			t.Errorf("after removing: %v", all)
		}

		for _, c := range []struct {
			folder string
			fi     FolderIcon
			want   error
		}{
			{"Nope", FolderIcon{Icon: "house"}, ErrNotFound},
			{"Projects", FolderIcon{Icon: "lucide:nope"}, ErrInvalidPath},
			{"Projects", FolderIcon{Color: "teal"}, ErrInvalidPath},
			{"", FolderIcon{Icon: "house"}, ErrInvalidPath},
		} {
			if _, err := v.SetFolderIcon(c.folder, c.fi); !errors.Is(err, c.want) {
				t.Errorf("SetFolderIcon(%q, %+v) = %v, want %v", c.folder, c.fi, err, c.want)
			}
		}
		if v.Ignored(FolderIconsFile, false) {
			t.Error("folders.yaml is ignored; it must sync")
		}
	})
}
