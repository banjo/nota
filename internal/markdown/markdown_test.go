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
