package publish

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestTokensInSync(t *testing.T) {
	src, err := os.ReadFile("../../packages/ui/src/tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	if string(src) != tokensCSS {
		t.Fatal("internal/publish/tokens.css is out of date: run go generate ./internal/publish")
	}
}

func TestThemeCheck(t *testing.T) {
	ok := Theme{Mode: "dark", Tokens: map[string]string{"background": "#101010", "ink-soft": "rgba(95, 217, 196, 0.14)"}}
	if err := CheckTheme(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Theme{
		{Mode: "sepia"},
		{Tokens: map[string]string{"font": "#fff"}},
		{Tokens: map[string]string{"background": "red;}body{display:none"}},
		{Tokens: map[string]string{"background": "url(https://x)"}},
	} {
		if CheckTheme(bad) == nil {
			t.Errorf("%+v passed", bad)
		}
	}
}

func TestPages(t *testing.T) {
	var b bytes.Buffer
	th := &Theme{Mode: "dark", Tokens: map[string]string{"ink": "#ff0000"}}
	if err := WritePage(&b, Page{Title: "Plan <1>", Body: "<p>hej</p>", Theme: th, Home: "https://nota.example"}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{`data-mode="dark"`, `--ink:#ff0000;`, `<title>Plan &lt;1&gt;</title>`, `<p>hej</p>`, `.callout-note .callout-icon{--icon:url("data:image/svg+xml,`, `href="https://nota.example"`} {
		if !strings.Contains(out, want) {
			t.Errorf("page: missing %s", want)
		}
	}
	b.Reset()
	if err := WriteLock(&b, Lock{Theme: th, Error: "Wrong password"}); err != nil {
		t.Fatal(err)
	}
	if out := b.String(); !strings.Contains(out, `role="alert">Wrong password`) || !strings.Contains(out, `method="post"`) {
		t.Error(out)
	}
	b.Reset()
	if err := WriteMissing(&b, Missing{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `<html lang="en">`) {
		t.Error("missing page has a mode")
	}
}
