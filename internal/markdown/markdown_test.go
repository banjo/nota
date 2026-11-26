package markdown

import (
	"reflect"
	"testing"
)

func TestParseTags(t *testing.T) {
	body := "# Rubrik\n\nText #idé and #projekt/nota, (#paren) #2026 #a-b_c\n" +
		"Not tags: foo#bar, http://x.se/#frag, ##double\n" +
		"`#inline` code\n\n```go\n#define X [[NotALink]]\n```\n\n    #indented code\n\n" +
		"<div>#html</div>\n\n#IDÉ again, trailing #slash/\n"
	got := Parse([]byte(body), "").Tags
	want := []string{"idé", "projekt/nota", "paren", "a-b_c", "slash"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %q, want %q", got, want)
	}
}

func TestParseLinks(t *testing.T) {
	body := "See [[Möte med Anna]], [[projekt/Plan|planen]], [[Plan#Mål]] and ![[bild.png]].\n" +
		"Also [rel](../Other%20Note.md#sec), [abs](/Root.md), [web](https://x.se/a.md), [img](pic.png).\n" +
		"`[[code]]` and [[#local heading]] are skipped.\n"
	got := Parse([]byte(body), "notes/sub").Links
	want := []Link{
		{Target: "Möte med Anna", Wiki: true},
		{Target: "projekt/Plan", Alias: "planen", Wiki: true},
		{Target: "Plan", Heading: "Mål", Wiki: true},
		{Target: "bild.png", Embed: true, Wiki: true},
		{Target: "notes/Other Note.md", Heading: "sec"},
		{Target: "Root.md"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links =\n%#v\nwant\n%#v", got, want)
	}
}

func TestCleanTag(t *testing.T) {
	for in, want := range map[string]string{"#a/b/": "a/b", " x ": "x", "123": "", "#": "", "a b": "", "1a": "1a"} {
		if got := CleanTag(in); got != want {
			t.Errorf("CleanTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRefs(t *testing.T) {
	body := "[[ Plan #Mål| planen ]] ![[bild.png]] [[a/B.md]] | [[T\\|alias]] |\n" +
		"[x](../Other%20Note.md#sec) ![i](<img/a b.png>) [w](https://x.se) `[[code]]`\n"
	got := Refs([]byte(body), "notes/sub")
	type ref struct {
		raw, target    string
		wiki, file, ag bool
	}
	want := []ref{
		{"Plan", "Plan", true, false, false},
		{"bild.png", "bild.png", true, false, false},
		{"a/B.md", "a/B.md", true, false, false},
		{"T", "T", true, false, false},
		{"../Other%20Note.md", "notes/Other Note.md", false, false, false},
		{"img/a b.png", "notes/sub/img/a b.png", false, true, true},
	}
	if len(got) != len(want) {
		t.Fatalf("refs = %#v", got)
	}
	for i, r := range got {
		w := want[i]
		if r.Raw != w.raw || body[r.Start:r.End] != w.raw || r.Target != w.target || r.Wiki != w.wiki || r.File != w.file || r.Angle != w.ag {
			t.Errorf("ref %d = %+v, want %+v", i, r, w)
		}
	}
	if got[0].Heading != "Mål" || got[0].Alias != "planen" || !got[1].Embed {
		t.Errorf("parts: %+v %+v", got[0], got[1])
	}
}

func TestParseWikilinkInTable(t *testing.T) {
	body := "| Note | Link |\n|---|---|\n| a | [[projekt/Plan\\|planen]] |\n"
	got := Parse([]byte(body), "").Links
	want := []Link{{Target: "projekt/Plan", Alias: "planen", Wiki: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("links =\n%#v\nwant\n%#v", got, want)
	}
}
