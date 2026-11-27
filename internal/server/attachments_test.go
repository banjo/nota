package server

import (
	"testing"

	"github.com/banjo/nota/internal/attachments"
)

func TestAttachments(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")
	var l attachments.List
	if st, m := b.call("GET", api+"/attachments", nil, &l); st != 200 || l.Folder != "" || len(l.Files) != 0 {
		t.Fatalf("empty: %d %v %+v", st, m, l)
	}
	for p, c := range map[string]string{
		"Attachments/bild.png":   "\x89PNG",
		"Attachments/gammal.png": "\x89PNG",
		"Plan.md":                "# Plan\n\n![](Attachments/bild.png)\n",
	} {
		if st, _, perr := e.put(tok, p, 0, c); st != 200 {
			t.Fatalf("put %s: %d %v", p, st, perr)
		}
	}
	if st, m := b.call("GET", api+"/attachments", nil, &l); st != 200 || l.Folder != "Attachments" || len(l.Files) != 2 {
		t.Fatalf("list: %d %v %+v", st, m, l)
	}
	for _, f := range l.Files {
		want := 0
		if f.Name == "bild.png" {
			want = 1
		}
		if len(f.UsedBy) != want || !f.Image {
			t.Errorf("%+v", f)
		}
	}
	var tree Tree
	if st, _ := b.call("GET", api+"/tree", nil, &tree); st != 200 || tree.Attachments != "Attachments" {
		t.Errorf("tree: %d %+v", st, tree.Attachments)
	}
}
