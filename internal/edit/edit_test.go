package edit

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	now := time.Date(2026, 11, 22, 10, 42, 0, 0, time.UTC)
	got, err := New("# Plan", []string{"a, b", "A"}, map[string]any{"status": "draft", "num": 2.0}, now)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\ntags: [a, b]\nnum: 2\nstatus: draft\ncreated: 2026-11-22T10:42:00Z\n---\n# Plan\n"
	if string(got) != want {
		t.Fatalf("New =\n%s\nwant\n%s", got, want)
	}
	// created from the text wins.
	got, _ = New("---\ncreated: 2020-01-01\n---\nx", nil, nil, now)
	if string(got) != "---\ncreated: 2020-01-01\n---\nx\n" {
		t.Fatalf("kept created: %q", got)
	}
	if _, err := New("x", []string{"!"}, nil, now); !errors.Is(err, ErrInvalidTag) {
		t.Fatalf("bad tag: %v", err)
	}
}

func TestTags(t *testing.T) {
	src := []byte("---\ntags: [a, b] # keep\nx: 1\n---\nbody #inline\n")
	out, tags, err := Tags(src, []string{"#c", "A"}, []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "---\ntags: [a, c]\nx: 1\n---\nbody #inline\n" || !slices.Equal(tags, []string{"a", "c"}) {
		t.Fatalf("Tags = %q %q", out, tags)
	}
	out, _, _ = Tags(out, nil, []string{"a", "c", "inline"})
	if string(out) != "---\nx: 1\n---\nbody #inline\n" {
		t.Fatalf("remove all: %q", out)
	}
	same, _, _ := Tags(out, nil, []string{"missing"})
	if string(same) != string(out) {
		t.Fatal("no-op changed the file")
	}
	if _, _, err := Tags([]byte("---\ntags: [x\n---\n"), []string{"y"}, nil); err == nil {
		t.Fatal("broken frontmatter should be refused")
	}
}

func TestBodyEdits(t *testing.T) {
	raw := []byte("---\na: 1\n---\nold\n")
	if got := ReplaceBody(raw, "new\n"); string(got) != "---\na: 1\n---\nnew\n" {
		t.Fatalf("ReplaceBody = %q", got)
	}
	if got := ReplaceBody([]byte("plain"), "new"); string(got) != "new" {
		t.Fatalf("ReplaceBody plain = %q", got)
	}
	if got := Append([]byte("x"), "- y"); string(got) != "x\n- y\n" {
		t.Fatalf("Append = %q", got)
	}
	if got := Append(nil, "\nz\n"); string(got) != "\nz\n" {
		t.Fatalf("Append empty = %q", got)
	}
}

func TestNormalize(t *testing.T) {
	got := Normalize(map[string]any{"a": 2.0, "b": 2.5, "c": []any{1.0, "x"}})
	m := got.(map[string]any)
	if m["a"] != int64(2) || m["b"] != 2.5 || m["c"].([]any)[0] != int64(1) {
		t.Fatalf("Normalize = %#v", got)
	}
}
