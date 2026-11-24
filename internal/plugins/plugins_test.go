package plugins

import (
	"strings"
	"testing"

	"github.com/banjo/nota/internal/vault"
)

const sunset = `{
  "id": "sunset",
  "name": "Sunset",
  "version": "1.0.0",
  "contributes": {
    "themes": [{
      "id": "warm",
      "name": "Warm",
      "light": { "background": "#fff8f0", "text": "#3a2a20", "ink": "#c2410c", "md-heading": "rgb(194, 65, 12)" },
      "dark": { "background": "#1f1612", "text": "#f2e6dc", "ink": "#fb923c", "ink-soft": "#fb923c26" }
    }]
  }
}`

func open(t *testing.T, files map[string]string) *vault.Vault {
	t.Helper()
	v, err := vault.OpenFS(vault.NewMemFS(files), "test", vault.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLoad(t *testing.T) {
	v := open(t, map[string]string{
		"Note.md":                             "x",
		".nota/plugins/sunset/manifest.json":  sunset,
		".nota/plugins/Night/manifest.json":   `{"contributes":{"themes":[{"id":"night","dark":{"background":"#000","text":"#fff","ink":"#8be9fd"}}]}}`,
		".nota/plugins/empty/README.md":       "no manifest",
		".nota/plugins/broken/manifest.json":  `{"id": "broken",`,
		".nota/plugins/copy/manifest.json":    `{"id": "sunset"}`,
		".nota/plugins/.hidden/manifest.json": `{}`,
		".nota/plugins/stray.json":            `{}`,
	})
	ps, err := Load(v)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range ps {
		ids = append(ids, p.ID+"@"+p.Dir)
	}
	want := []string{
		"broken@.nota/plugins/broken",
		"empty@.nota/plugins/empty",
		"night@.nota/plugins/Night",
		"sunset@.nota/plugins/sunset",
		"sunset@.nota/plugins/copy",
	}
	if strings.Join(ids, " ") != strings.Join(want, " ") {
		t.Fatalf("plugins %v, want %v", ids, want)
	}
	if !strings.Contains(ps[0].Error, "manifest.json") {
		t.Errorf("broken: %q", ps[0].Error)
	}
	if ps[1].Error != "no manifest.json" {
		t.Errorf("empty: %q", ps[1].Error)
	}
	night := ps[2]
	if night.Error != "" || night.Name != "night" || len(night.Contributes.Themes) != 1 {
		t.Fatalf("night: %+v", night)
	}
	if th := night.Contributes.Themes[0]; th.Name != "night" || th.Light != nil || th.Dark["ink"] != "#8be9fd" {
		t.Errorf("night theme: %+v", th)
	}
	// Two folders with the same id: the one named after it keeps it.
	if ps[3].Error != "" || len(ps[3].Contributes.Themes) != 1 {
		t.Errorf("sunset: %+v", ps[3])
	}
	if ps[4].Error != `the id "sunset" is taken by .nota/plugins/sunset` {
		t.Errorf("duplicate message: %q", ps[4].Error)
	}
	for _, p := range ps {
		if p.Contributes.Themes == nil {
			t.Errorf("%s: themes is nil", p.Dir)
		}
	}
}

func TestLoadNoDir(t *testing.T) {
	ps, err := Load(open(t, map[string]string{"A.md": "a"}))
	if err != nil || ps == nil || len(ps) != 0 {
		t.Fatalf("Load = %v, %v", ps, err)
	}
}

func TestParse(t *testing.T) {
	p, err := Parse([]byte(sunset), "sunset")
	if err != nil {
		t.Fatal(err)
	}
	th := p.Contributes.Themes[0]
	if p.Version != "1.0.0" || th.ID != "warm" || th.Light["md-heading"] != "rgb(194, 65, 12)" {
		t.Errorf("parsed %+v", p)
	}

	bad := map[string]string{
		`{"id": "Sun Set"}`: `id "Sun Set"`,
		`{"nmae": "x"}`:     `unknown field "nmae"`,
		`{} {}`:             "more than one JSON value",
		`{"contributes": {"themes": [{"id": "a"}]}}`:                                                                                                                                            `needs "light", "dark" or both`,
		`{"contributes": {"themes": [{"light": {}}]}}`:                                                                                                                                          `theme 1: id ""`,
		`{"contributes": {"themes": [{"id": "a", "dark": {"background": "#000", "text": "#fff"}}]}}`:                                                                                            "dark: ink is required",
		`{"contributes": {"themes": [{"id": "a", "dark": {"background": "#000", "text": "#fff", "ink": "#fff8"}}]}}`:                                                                            "without alpha",
		`{"contributes": {"themes": [{"id": "a", "dark": {"background": "#000", "text": "#fff", "ink": "#fff", "backgrond": "#000"}}]}}`:                                                        `unknown token "backgrond"`,
		`{"contributes": {"themes": [{"id": "a", "light": {"background": "#000", "text": "#fff", "ink": "#fff", "hover": "url(https://x)"}}]}}`:                                                 "hover: \"url(https://x)\" is not a colour",
		`{"contributes": {"themes": [{"id": "a", "light": {"background": "red", "text": "#fff", "ink": "#fff"}}]}}`:                                                                             "background: \"red\" is not a colour",
		`{"contributes": {"themes": [{"id": "a", "dark": {"background": "#000", "text": "#fff", "ink": "#fff"}}, {"id": "a", "dark": {"background": "#000", "text": "#fff", "ink": "#fff"}}]}}`: "used twice",
	}
	for in, want := range bad {
		_, err := Parse([]byte(in), "x")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%s) = %v, want %q", in, err, want)
		}
	}
}

func TestIsColor(t *testing.T) {
	for _, s := range []string{"#fff", "#ffff", "#a1b2c3", "#a1b2c3d4", "rgb(1, 2, 3)", "rgba(1 2 3 / 50%)", "hsl(210deg 40% 50%)"} {
		if !IsColor(s) {
			t.Errorf("IsColor(%q) = false", s)
		}
	}
	for _, s := range []string{"", "red", "#ggg", "#12345", "var(--x)", "url(x)", "rgb(1,2,3);color:red", "expression(1)"} {
		if IsColor(s) {
			t.Errorf("IsColor(%q) = true", s)
		}
	}
}
