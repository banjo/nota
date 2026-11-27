package publish

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCard(t *testing.T) {
	long := strings.Repeat("Mötesanteckningar från planeringen av nästa kvartal ", 6)
	cards := map[string]Card{
		"plain": {Title: "Plan för Q3 🚀", Excerpt: "Vi börjar med det som blockerar flest: synken av bilagor, sedan delning med lösenord och till sist importen från Notion.", Footer: "Updated 12 March 2026", Site: "nota.example.com"},
		"dark":  {Title: "Reading list", Excerpt: "Books and papers I want to get to this winter.", Footer: "Updated 1 May 2026", Site: "nota.example.com", Theme: &Theme{Mode: "dark"}},
		"long":  {Title: long, Excerpt: long, Footer: "Updated 1 May 2026", Site: "nota.example.com", Theme: &Theme{Tokens: map[string]string{"ink": "rgb(200, 60, 40)", "background": "#fbf7ef"}}},
		"word":  {Title: "Supercalifragilisticexpialidocious-and-then-some-more-letters-to-break", Footer: "Updated 1 May 2026"},
		"cjk":   {Title: "会議メモ", Excerpt: "", Site: "nota.example.com"},
		"lock":  {Title: "Protected note", Excerpt: "Open the link and enter the password you were given to read it.", Site: "nota.example.com", Theme: &Theme{Mode: "dark"}},
	}
	dir := os.Getenv("NOTA_CARD_DIR") // set to look at the cards
	for name, c := range cards {
		var b bytes.Buffer
		if err := WriteCard(&b, c); err != nil {
			t.Fatal(name, err)
		}
		img, err := png.Decode(bytes.NewReader(b.Bytes()))
		if err != nil {
			t.Fatal(name, err)
		}
		if sz := img.Bounds().Size(); sz.X != CardWidth || sz.Y != CardHeight {
			t.Errorf("%s: %v", name, sz)
		}
		if dir != "" {
			_ = os.WriteFile(filepath.Join(dir, name+".png"), b.Bytes(), 0o644)
		}
	}
}

func TestParseColor(t *testing.T) {
	for in, want := range map[string][4]uint8{
		"#fff":                    {255, 255, 255, 255},
		"#1e7363":                 {0x1e, 0x73, 0x63, 255},
		"#1e736380":               {0x1e, 0x73, 0x63, 0x80},
		"rgb(95, 217, 196)":       {95, 217, 196, 255},
		"rgba(95, 217, 196, 0.5)": {95, 217, 196, 128},
		"rgb(95 217 196 / 50%)":   {95, 217, 196, 128},
	} {
		c, ok := parseColor(in)
		if !ok || [4]uint8{c.R, c.G, c.B, c.A} != want {
			t.Errorf("%s: %v %v", in, c, ok)
		}
	}
	if _, ok := parseColor("hsl(0 0% 0%)"); ok {
		t.Error("hsl parsed")
	}
}

func TestExcerpt(t *testing.T) {
	body := Render([]byte("Första stycket med **fet** text.\n\n```go\nkod()\n```\n\n- en\n- två[^1]\n\n[^1]: fotnot\n"), Options{})
	if got := Excerpt(body, 200); got != "Första stycket med fet text. en två" {
		t.Errorf("%q", got)
	}
	if got := Excerpt(body, 20); got != "Första stycket med…" {
		t.Errorf("%q", got)
	}
}
