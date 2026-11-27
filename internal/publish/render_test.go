package publish

import (
	"strings"
	"testing"

	"github.com/banjo/nota/internal/markdown"
)

type stub struct{}

func (stub) Note(l markdown.Link) (string, bool) {
	if l.Target == "Shared" || l.Target == "Projekt/Shared.md" {
		return "/s/abc", true
	}
	return "", false
}

func (stub) Image(target string, wiki bool) (string, bool) {
	if target == "secret.png" {
		return "", false
	}
	return "/s/me/f/" + target, true
}

const note = `---
tags: [x]
secret: frontmatter
---
# Planen

Text med ==markering==, #projekt/nota och <kbd>⌘K</kbd>, men <script>alert(1)</script>.
Se [[Shared]], [[Shared#Mål & plan|målen]], [[Privat]] och [md](Shared.md) och [webb](https://example.com).
%%dold kommentar [[Shared]]%%
Ett block ^abc123

![[bild.png|300]]
![skiss|200](../Attachments/skiss.png)
![[secret.png]]
![[Shared]]
![extern](https://example.com/x.png)

> [!warning] Se upp
> Brödtext **fet**.

> [!tip]-
> Hopfälld

Fotnot^[inline text] och [^1].

[^1]: Vanlig.

` + "```\n[[Shared]] ==inte== #kod %%kvar%%\n```\n"

func TestRender(t *testing.T) {
	out := string(Render([]byte(note), Options{Path: "Projekt/Plan.md", Resolver: stub{}}))
	for _, want := range []string{
		`<h1 id="planen">Planen</h1>`,
		`<mark>markering</mark>`,
		`<span class="tag">#projekt/nota</span>`,
		`<kbd>⌘K</kbd>`,
		`&lt;script&gt;alert(1)&lt;/script&gt;`,
		`<a href="/s/abc" class="internal">Shared</a>`,
		`<a href="/s/abc#m%C3%A5l--plan" class="internal">målen</a>`,
		`<span class="unresolved">Privat</span>`,
		`<a href="/s/abc" class="internal">md</a>`,
		`<a href="https://example.com" rel="noopener noreferrer nofollow">webb</a>`,
		`Ett block</p>`,
		`<img src="/s/me/f/bild.png" alt="" loading="lazy" width="300" />`,
		`<img src="/s/me/f/Attachments/skiss.png" alt="skiss" loading="lazy" width="200" />`,
		`<a href="/s/abc" class="internal embed">Shared</a>`,
		`<a href="https://example.com/x.png" rel="noopener noreferrer nofollow">extern</a>`,
		`<div class="callout callout-warning"><p class="callout-title"><svg class="callout-icon" aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor"`, `<path d="M12 9v4"/>`, `</svg>Se upp</p><div class="callout-body">`,
		`<strong>fet</strong>`,
		`<details class="callout callout-tip"><summary class="callout-title">`,
		`class="footnote-ref"`,
		"<code>[[Shared]] ==inte== #kod %%kvar%%\n</code>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s", want)
		}
	}
	for _, not := range []string{"frontmatter", "dold kommentar", "^abc123", "secret.png", "<script>"} {
		if strings.Contains(out, not) {
			t.Errorf("should not contain %q", not)
		}
	}
	if t.Failed() {
		t.Log(out)
	}
}

func TestRenderWideImage(t *testing.T) {
	out := string(Render([]byte("![[a.png]]\n\n![[b.png|300]]\n\nText ![[c.png]]\n\n> ![[d.png]]\n"), Options{Resolver: stub{}}))
	for _, want := range []string{
		`<p class="wide"><img src="/s/me/f/a.png"`,
		`<p><img src="/s/me/f/b.png" alt="" loading="lazy" width="300" /></p>`,
		`<p>Text <img src="/s/me/f/c.png"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	if strings.Count(out, `class="wide"`) != 2 {
		t.Errorf("wide: %s", out)
	}
}

func TestRenderNote(t *testing.T) {
	title, body := RenderNote([]byte("---\na: 1\n---\n# Planen\n\nText.\n"), Options{Path: "P/Plan.md"})
	if title != "Planen" || strings.Contains(string(body), "Planen") || !strings.Contains(string(body), "<p>Text.</p>") {
		t.Fatalf("%q %q", title, body)
	}
	if title, _ := RenderNote([]byte("\nText.\n"), Options{Path: "P/Plan.md"}); title != "Plan" {
		t.Fatalf("empty first line: %q", title)
	}
}

func TestSlug(t *testing.T) {
	if got := Slug("Mål & plan!"); got != "mål--plan" {
		t.Fatal(got)
	}
}
