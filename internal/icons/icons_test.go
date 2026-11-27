package icons

import (
	"strings"
	"testing"
)

func TestSVG(t *testing.T) {
	for _, id := range []string{"lucide:briefcase", "tabler:home", "simple-icons:github", "briefcase", "pen", "check", "lucide:activity-square"} {
		svg, err := SVG(id)
		if err != nil {
			t.Errorf("SVG(%q): %v", id, err)
			continue
		}
		if !strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg" width="1em" height="1em" viewBox="0 0 24 24">`) || !strings.Contains(svg, "currentColor") {
			t.Errorf("SVG(%q) = %s", id, svg)
		}
	}
	for _, id := range []string{"🏠", "❤️", "❤"} {
		if svg, err := SVG(id); err != nil || svg != "" {
			t.Errorf("SVG(%q) = %q, %v; want an emoji", id, svg, err)
		}
	}
	for _, id := range []string{"", "lucide:nope", "fa:house", "hej", "🏠🏠"} {
		if err := Check(id); err == nil {
			t.Errorf("Check(%q) = nil, want an error", id)
		}
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"house":          "lucide:house",
		" lucide:house ": "lucide:house",
		"pen":            "lucide:pen-line",
		"tabler:home":    "tabler:home",
		"🏠":              "🏠",
		"":               "",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearch(t *testing.T) {
	ids := func(q, set string) []string {
		t.Helper()
		ics, err := Search(q, set, 400)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, ic := range ics {
			if ic.Set != EmojiSet && ic.SVG == "" {
				t.Errorf("%s has no SVG", ic.ID)
			}
			out = append(out, ic.ID)
		}
		return out
	}
	if got := ids("house", ""); len(got) < 3 || got[0] != "lucide:house" || got[1] != "🏠" || !contains(got, "tabler:building-warehouse") {
		t.Errorf("house: %v", got[:min(5, len(got))])
	}
	if got := ids("github", ""); len(got) == 0 || got[0] != "simple-icons:github" || !contains(got, "tabler:brand-github") {
		t.Errorf("github: %v", got)
	}
	if got := ids("house", EmojiSet); len(got) == 0 || got[0] != "🏠" {
		t.Errorf("emoji house: %v", got)
	}
	if got := ids("thumbs up", EmojiSet); len(got) != 1 {
		t.Errorf("skin tones are left out unless asked for: %v", got)
	}
	if got := ids("arrow down", "lucide"); len(got) == 0 || !strings.HasPrefix(got[0], "lucide:arrow-down") {
		t.Errorf("arrow down: %v", got[:min(5, len(got))])
	}
	if got := ids("", ""); len(got) < 40 || got[0] != "lucide:folder" {
		t.Errorf("suggestions: %v", got)
	}
	if got := ids("zzzzqqq", ""); len(got) != 0 {
		t.Errorf("nonsense: %v", got)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestCheckColor(t *testing.T) {
	if CheckColor("") != nil || CheckColor("blue") != nil || CheckColor("teal") == nil {
		t.Error("CheckColor")
	}
}
