package sync

import (
	"slices"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// hunk replaces the base lines [start, end) with lines. start == end is
// a pure insertion before base line start.
type hunk struct {
	start, end int
	lines      []string
}

func (h hunk) insertion() bool { return h.start == h.end }

func (h hunk) equal(o hunk) bool {
	return h.start == o.start && h.end == o.end && slices.Equal(h.lines, o.lines)
}

// splitLines splits s after every "\n"; a last line without one is kept.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// lineHunks diffs base against other line by line.
func lineHunks(base, other string) []hunk {
	dmp := diffmatchpatch.New()
	a, b, lines := dmp.DiffLinesToRunes(base, other)
	diffs := dmp.DiffMainRunes(a, b, false)
	var out []hunk
	var cur *hunk
	pos := 0
	flush := func() {
		if cur != nil {
			out = append(out, *cur)
			cur = nil
		}
	}
	for _, d := range diffs {
		rs := []rune(d.Text)
		switch d.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			pos += len(rs)
		case diffmatchpatch.DiffDelete:
			if cur == nil {
				cur = &hunk{start: pos, end: pos}
			}
			pos += len(rs)
			cur.end = pos
		case diffmatchpatch.DiffInsert:
			if cur == nil {
				cur = &hunk{start: pos, end: pos}
			}
			for _, r := range rs {
				cur.lines = append(cur.lines, lines[r])
			}
		}
	}
	flush()
	return out
}

// overlaps reports whether two hunks touch the same part of the base so
// that their order or combination is ambiguous. Changes to different
// lines, even adjacent ones, do not overlap; an insertion overlaps a
// change at the same spot.
func overlaps(a, b hunk) bool {
	if a.insertion() || b.insertion() {
		return a.start <= b.end && b.start <= a.end
	}
	return a.start < b.end && b.start < a.end
}

// Merge3 merges two edits of base line by line, like diff3: changes from
// local and remote to different parts of base are both applied; the same
// change made on both sides is applied once. When both sides changed the
// same lines differently, ok is false and the caller must keep both
// versions (a conflict copy).
func Merge3(base, local, remote string) (merged string, ok bool) {
	switch {
	case local == remote, remote == base:
		return local, true
	case local == base:
		return remote, true
	}
	bl := splitLines(base)
	type side struct {
		hunks []hunk
		i     int
	}
	l := &side{hunks: lineHunks(base, local)}
	r := &side{hunks: lineHunks(base, remote)}
	var out strings.Builder
	pos := 0
	emit := func(h hunk) {
		for _, s := range bl[pos:h.start] {
			out.WriteString(s)
		}
		for _, s := range h.lines {
			out.WriteString(s)
		}
		pos = h.end
	}
	for l.i < len(l.hunks) || r.i < len(r.hunks) {
		switch {
		case r.i == len(r.hunks):
			emit(l.hunks[l.i])
			l.i++
			continue
		case l.i == len(l.hunks):
			emit(r.hunks[r.i])
			r.i++
			continue
		}
		a, b := l.hunks[l.i], r.hunks[r.i]
		switch {
		case a.equal(b):
			emit(a)
			l.i++
			r.i++
		case overlaps(a, b):
			return "", false
		case a.start < b.start || (a.start == b.start && a.end <= b.end):
			emit(a)
			l.i++
		default:
			emit(b)
			r.i++
		}
	}
	for _, s := range bl[pos:] {
		out.WriteString(s)
	}
	return out.String(), true
}
