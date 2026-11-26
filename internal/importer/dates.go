package importer

import (
	"slices"
	"strings"
	"time"

	"github.com/banjo/nota/internal/frontmatter"
)

// Frontmatter keys other apps keep a note's dates in. They become
// Nota's `created` and `updated` (an RFC 3339 time), and the other
// app's key goes. Keys such as `date` (a blog post's date) mean
// something else and stay.
var (
	createdKeys = []string{"created", "date created", "created_at", "createdat", "creation date", "created time", "created on"}
	updatedKeys = []string{"updated", "modified", "date modified", "updated_at", "updatedat", "last modified", "last_modified", "last edited time", "modified time", "updated on"}
)

var dateLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04",
	time.DateOnly,
	"2006/01/02 15:04",
	"2006/01/02",
	"January 2, 2006 3:04 PM", // Notion
	"January 2, 2006",
	"Jan 2, 2006 3:04 PM",
	"Jan 2, 2006",
}

// parseDate reads a frontmatter date in local time.
func parseDate(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		s := strings.TrimSpace(x)
		for _, l := range dateLayouts {
			if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// dates returns a note with `created` and `updated` in Nota's format:
// taken from the keys above, else from the file's times (created, or
// modified when the source has no creation time). A note whose
// frontmatter does not parse is left as it is, and so is a `created` or
// `updated` that is not a date.
func dates(data []byte, created, modified time.Time) []byte {
	props, err := frontmatter.Split(data).Properties()
	if err != nil {
		return data
	}
	if created.IsZero() {
		created = modified
	}
	for _, d := range []struct {
		name string
		keys []string
		file time.Time
	}{{"created", createdKeys, created}, {"updated", updatedKeys, modified}} {
		var when time.Time
		var others []string
		keep, best := false, len(d.keys)
		for k, v := range props {
			i := slices.Index(d.keys, strings.ToLower(strings.TrimSpace(k)))
			if i < 0 {
				continue
			}
			t, ok := parseDate(v)
			switch {
			case k == d.name && !ok:
				keep = true // Nota's key, but not a date: leave it be
			case !ok:
			default:
				if k != d.name {
					others = append(others, k)
				}
				if i < best {
					when, best = t, i
				}
			}
		}
		if keep {
			continue
		}
		if when.IsZero() {
			when = d.file
		}
		if when.IsZero() {
			continue
		}
		for _, k := range others {
			data = frontmatter.Delete(data, k)
		}
		if out, err := frontmatter.SetLiteral(data, d.name, when.Format(time.RFC3339)); err == nil {
			data = out
		}
	}
	return data
}
