package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	v, warnings, err := Parse([]byte(`#:schema https://example.com/x.json
theme = "dark"
palette = "paper"
note_mode = "sideways"  # not a mode
properties = true       # not a string
future_key = "x"        # from a newer Nota
`))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Values{"theme": "dark", "palette": "paper"}); !reflect.DeepEqual(v, want) {
		t.Errorf("values = %v, want %v", v, want)
	}
	want := []string{
		`note_mode = "sideways" is not valid (want live, reading, source); it is ignored`,
		`properties must be a string; it is ignored`,
		`unknown setting "future_key" is ignored`,
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Errorf("warnings = %q, want %q", warnings, want)
	}
}

func TestParseBroken(t *testing.T) {
	if _, _, err := Parse([]byte("theme = dark\n")); err == nil {
		t.Fatal("bare word: want a TOML error")
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	v, w, err := Load(filepath.Join(dir, "missing.toml"))
	if err != nil || len(v) != 0 || w != nil {
		t.Fatalf("missing file = %v, %v, %v; want nothing set", v, w, err)
	}
	p := filepath.Join(dir, File)
	if err := os.WriteFile(p, []byte("theme = \"light\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if v, _, err = Load(p); err != nil || v["theme"] != "light" {
		t.Fatalf("Load = %v, %v", v, err)
	}
	if err := os.WriteFile(p, []byte("theme = \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Load(p); err == nil || !strings.Contains(err.Error(), p) {
		t.Fatalf("broken file: err = %v, want one naming the file", err)
	}
}

func TestPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if p, _ := Path(); p != filepath.Join("/cfg", "nota", "settings.toml") {
		t.Errorf("Path = %s", p)
	}
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if p, _ := Path(); p != filepath.Join(home, ".config", "nota", "settings.toml") {
		t.Errorf("Path = %s", p)
	}
}

func TestMerge(t *testing.T) {
	got := Merge(
		Layer{SourceDevice, Values{"theme": "light", "note_mode": "source"}},
		Layer{SourceAccount, Values{"theme": "dark", "palette": "paper", "note_mode": "nope"}},
		Layer{SourceFile, Values{"palette": "nord"}},
	)
	want := []Value{
		{"theme", "dark", SourceAccount},
		{"palette", "nord", SourceFile},
		{"note_mode", "source", SourceDevice}, // the account's value is not a mode
		{"properties", "hide", SourceDefault},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Merge = %v, want %v", got, want)
	}
}

func TestTemplateParses(t *testing.T) {
	v, w, err := Parse([]byte(Template()))
	if err != nil || len(v) != 0 || len(w) != 0 {
		t.Fatalf("Template parses to %v, %v, %v; want nothing set", v, w, err)
	}
	// Uncommenting a line sets that key to its default.
	for _, k := range Keys {
		line := "# " + k.Name + " = "
		if !strings.Contains(Template(), line) {
			t.Errorf("template has no line for %s", k.Name)
		}
	}
	all := Defaults()
	v, w, err = Parse([]byte(Encode(all)))
	if err != nil || len(w) != 0 || !reflect.DeepEqual(v, all) {
		t.Fatalf("Encode round trip = %v, %v, %v", v, w, err)
	}
}

// The landing site serves the schema; it must match Keys.
func TestSchemaFile(t *testing.T) {
	p := filepath.Join("..", "..", "apps", "landing", "public", "settings.schema.json")
	if os.Getenv("NOTA_UPDATE_SCHEMA") != "" {
		if err := os.WriteFile(p, Schema(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(Schema()) {
		t.Errorf("%s is out of date; run NOTA_UPDATE_SCHEMA=1 go test ./internal/settings", p)
	}
}

func TestValidName(t *testing.T) {
	for _, n := range Names() {
		if !ValidName(n) {
			t.Errorf("%s is not a valid name", n)
		}
	}
	for _, n := range []string{"", "Theme", "1x", "a-b", strings.Repeat("a", 65)} {
		if ValidName(n) {
			t.Errorf("%q should not be a valid name", n)
		}
	}
}
