package server

import (
	"strings"
	"testing"

	"github.com/banjo/nota/internal/icons"
	"github.com/banjo/nota/internal/protocol"
)

func TestWebAPIIcons(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")
	for p, c := range map[string]string{
		"Projekt/Plan.md":         "# Plan\n",
		".nota/views/drafts.yaml": "name: Drafts\nicon: pen\nwhere: { status: draft }\n",
	} {
		if st, _, err := e.put(tok, p, 0, c); st != 200 {
			t.Fatalf("put %s: %d %v", p, st, err)
		}
	}

	var l icons.Look
	if st, m := b.call("PUT", api+"/folders/icon", IconRequest{Path: "Projekt", Icon: "briefcase", Color: "blue"}, &l); st != 200 ||
		l.Icon != "lucide:briefcase" || l.Color != "blue" || !strings.HasPrefix(l.SVG, "<svg") {
		t.Fatalf("set folder icon: %d %v %+v", st, m, l)
	}
	// It is a version of the web device, so it syncs.
	res, body := e.do(tok, "GET", protocol.FilePath("main", ".nota/folders.yaml"), nil, "")
	if res.StatusCode != 200 || string(body) != "Projekt:\n  icon: lucide:briefcase\n  color: blue\n" || res.Header.Get(protocol.HeaderDevice) != DeviceWeb {
		t.Fatalf("folders.yaml: %d %q", res.StatusCode, body)
	}
	var tree Tree
	if st, m := b.call("GET", api+"/tree", nil, &tree); st != 200 || len(tree.Folders) != 1 ||
		tree.Folders[0].Icon != "lucide:briefcase" || tree.Folders[0].Color != "blue" || tree.Folders[0].SVG != l.SVG {
		t.Fatalf("tree: %d %v %+v", st, m, tree)
	}
	if st, m := b.call("PUT", api+"/folders/icon", IconRequest{Path: "Projekt", Icon: "lucide:nope"}, nil); st != 400 || code(m) != protocol.CodeInvalidPath {
		t.Fatalf("unknown icon: %d %v", st, m)
	}
	if st, m := b.call("PUT", api+"/folders/icon", IconRequest{Path: "Nej", Icon: "house"}, nil); st != 404 {
		t.Fatalf("missing folder: %d %v", st, m)
	}

	var vs struct{ Views []ViewInfo }
	if st, m := b.call("GET", api+"/views", nil, &vs); st != 200 || len(vs.Views) != 1 || vs.Views[0].Icon != "pen" || !strings.HasPrefix(vs.Views[0].SVG, "<svg") {
		t.Fatalf("views: %d %v %+v", st, m, vs)
	}
	var vl icons.Look
	if st, m := b.call("PUT", api+"/views/drafts/icon", IconRequest{Icon: "🏠", Color: "green"}, &vl); st != 200 || vl.Icon != "🏠" || vl.SVG != "" || vl.Color != "green" {
		t.Fatalf("set view icon: %d %v %+v", st, m, vl)
	}
	if st, m := b.call("PUT", api+"/views/nope/icon", IconRequest{Icon: "house"}, nil); st != 404 {
		t.Fatalf("missing view: %d %v", st, m)
	}

	var found struct{ Icons []icons.Icon }
	if st, m := b.call("GET", api+"/icons?q=house&set=lucide", nil, &found); st != 200 || len(found.Icons) == 0 || found.Icons[0].ID != "lucide:house" {
		t.Fatalf("icons: %d %v %+v", st, m, found)
	}
	if st, m := b.call("GET", api+"/icons?set=fa", nil, nil); st != 400 {
		t.Fatalf("unknown set: %d %v", st, m)
	}
}
