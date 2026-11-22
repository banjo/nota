// Package edit holds the note edits shared by the CLI, the MCP server,
// hooks and the app: building a new note, editing tags and properties in
// frontmatter, replacing or appending to the body. Every function works on
// the raw file bytes and changes only what it has to, so the rest of the
// file stays byte for byte (see package frontmatter).
package edit

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/markdown"
)

// ErrInvalidTag is returned for tags that are empty after cleaning.
var ErrInvalidTag = errors.New("invalid tag")

// CreatedKey is the frontmatter key new notes are stamped with.
const CreatedKey = "created"

// New builds the file of a new note: text, plus tags, props and a created
// timestamp in its frontmatter (merged into frontmatter text already has).
// Tags may be comma-separated lists. created is only added when text does
// not set it.
func New(text string, tags []string, props map[string]any, now time.Time) ([]byte, error) {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	src := []byte(text)
	var err error
	if len(tags) > 0 {
		var add []string
		for _, t := range tags {
			for _, one := range strings.Split(t, ",") {
				if strings.TrimSpace(one) != "" {
					add = append(add, one)
				}
			}
		}
		if src, _, err = Tags(src, add, nil); err != nil {
			return nil, err
		}
	}
	if len(props) > 0 {
		if src, err = Props(src, props, nil); err != nil {
			return nil, err
		}
	}
	p, _ := frontmatter.Split(src).Properties()
	if _, ok := p[CreatedKey]; !ok {
		if src, err = frontmatter.Set(src, CreatedKey, now.Format(time.RFC3339)); err != nil {
			return nil, err
		}
	}
	return src, nil
}

// Check refuses content whose frontmatter does not parse: a line-based
// edit could make it worse.
func Check(src []byte) error {
	if _, err := frontmatter.Split(src).Properties(); err != nil {
		return fmt.Errorf("frontmatter is not valid YAML, fix it first: %w", err)
	}
	return nil
}

// TagList reads the tags property as Obsidian does: a list, or a string of
// comma or space separated tags. Tags are cleaned and deduplicated
// ignoring case.
func TagList(v any) []string {
	var raw []string
	switch x := v.(type) {
	case []any:
		for _, t := range x {
			raw = append(raw, fmt.Sprint(t))
		}
	case []string:
		raw = x
	case string:
		raw = strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == ' ' })
	}
	out := []string{}
	for _, t := range raw {
		if c := markdown.CleanTag(t); c != "" {
			out = AddTag(out, c)
		}
	}
	return out
}

// AddTag appends t unless ts has it already, ignoring case.
func AddTag(ts []string, t string) []string {
	for _, x := range ts {
		if strings.EqualFold(x, t) {
			return ts
		}
	}
	return append(ts, t)
}

// RemoveTag returns ts without t, ignoring case.
func RemoveTag(ts []string, t string) []string {
	out := ts[:0:0]
	for _, x := range ts {
		if !strings.EqualFold(x, t) {
			out = append(out, x)
		}
	}
	return out
}

// Tags adds and removes tags in the frontmatter "tags" list and returns
// the new content and tag list. Removing the last tag removes the key;
// inline #tags in the body are not touched. Unchanged tags return src as
// is.
func Tags(src []byte, add, remove []string) ([]byte, []string, error) {
	if err := Check(src); err != nil {
		return nil, nil, err
	}
	props, _ := frontmatter.Split(src).Properties()
	before := TagList(props["tags"])
	after := append([]string{}, before...)
	for _, t := range remove {
		c := markdown.CleanTag(t)
		if c == "" {
			return nil, nil, fmt.Errorf("%w %q", ErrInvalidTag, t)
		}
		after = RemoveTag(after, c)
	}
	for _, t := range add {
		c := markdown.CleanTag(t)
		if c == "" {
			return nil, nil, fmt.Errorf("%w %q", ErrInvalidTag, t)
		}
		after = AddTag(after, c)
	}
	if equal(before, after) {
		return src, after, nil
	}
	if len(after) == 0 {
		return frontmatter.Delete(src, "tags"), after, nil
	}
	out, err := frontmatter.Set(src, "tags", after)
	return out, after, err
}

// Props sets and removes frontmatter keys. Values are normalized from
// JSON first: whole float64 numbers become integers, so 2 stays "2".
func Props(src []byte, set map[string]any, unset []string) ([]byte, error) {
	if err := Check(src); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var err error
	for _, k := range keys {
		if strings.TrimSpace(k) == "" || strings.ContainsAny(k, "\n\r") {
			return nil, fmt.Errorf("invalid key %q", k)
		}
		if src, err = frontmatter.Set(src, k, Normalize(set[k])); err != nil {
			return nil, fmt.Errorf("set %s: %w", k, err)
		}
	}
	for _, k := range unset {
		src = frontmatter.Delete(src, k)
	}
	return src, nil
}

// Normalize turns JSON-decoded values into what YAML should show: whole
// numbers as integers, recursively through lists and maps.
func Normalize(v any) any {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return int64(x)
		}
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = Normalize(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = Normalize(e)
		}
		return out
	}
	return v
}

// ReplaceBody returns raw with everything after the frontmatter replaced
// by body. The frontmatter block stays byte for byte.
func ReplaceBody(raw []byte, body string) []byte {
	d := frontmatter.Split(raw)
	head := raw[:len(raw)-len(d.Body())]
	out := append([]byte{}, head...)
	return append(out, body...)
}

// Append adds text at the end of the note on a line of its own and makes
// sure the file ends with a newline. Start text with an empty line for a
// new paragraph.
func Append(raw []byte, text string) []byte {
	out := append([]byte{}, raw...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, text...)
	if !strings.HasSuffix(text, "\n") {
		out = append(out, '\n')
	}
	return out
}

func equal(a, b []string) bool { return slices.Equal(a, b) }
