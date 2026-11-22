package vault

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

// DefaultIgnore lists what every vault ignores, in .notaignore syntax.
// Nota's own state in .nota/ (index, sync state, merge bases) stays
// local; the shared configuration in it (hooks.toml, views/, plugins/)
// is part of the vault and syncs.
var DefaultIgnore = []string{".git/", ".obsidian/", ".nota/*", "!.nota/hooks.toml", "!.nota/views/", "!.nota/plugins/",
	".trash/", ".DS_Store"}

// IgnoreFile is the name of the per-vault ignore file at the vault root.
const IgnoreFile = ".notaignore"

// Ignore matches vault-relative paths against gitignore-style rules.
//
// Supported syntax: blank lines and "#" comments, "!" negation, a trailing
// "/" for directories only, a leading or inner "/" to anchor the pattern at
// the vault root, and the globs "*", "?", "[...]" and "**". The last
// matching rule wins, and a path inside an ignored directory is ignored.
type Ignore struct{ rules []rule }

type rule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

// ParseIgnore builds a matcher from rule lines.
func ParseIgnore(lines []string) *Ignore {
	m := &Ignore{}
	for _, l := range lines {
		if r, ok := parseRule(l); ok {
			m.rules = append(m.rules, r)
		}
	}
	return m
}

// ParseIgnoreFile builds a matcher from DefaultIgnore followed by the
// contents of a .notaignore file.
func ParseIgnoreFile(data []byte) *Ignore {
	lines := append([]string{}, DefaultIgnore...)
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return ParseIgnore(lines)
}

func parseRule(l string) (rule, bool) {
	l = strings.TrimSuffix(l, "\r")
	// Trailing spaces are ignored unless escaped.
	for strings.HasSuffix(l, " ") && !strings.HasSuffix(l, `\ `) {
		l = l[:len(l)-1]
	}
	if l == "" || strings.HasPrefix(l, "#") {
		return rule{}, false
	}
	var r rule
	if strings.HasPrefix(l, "!") {
		r.negate = true
		l = l[1:]
	} else if strings.HasPrefix(l, `\!`) || strings.HasPrefix(l, `\#`) {
		l = l[1:]
	}
	if strings.HasSuffix(l, "/") {
		r.dirOnly = true
		l = strings.TrimRight(l, "/")
	}
	if l == "" {
		return rule{}, false
	}
	anchored := strings.Contains(l, "/")
	l = strings.TrimPrefix(l, "/")
	var b strings.Builder
	if anchored {
		b.WriteString("^")
	} else {
		b.WriteString("(?:^|/)")
	}
	b.WriteString(globToRegexp(l))
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return rule{}, false
	}
	r.re = re
	return r, true
}

func globToRegexp(g string) string {
	var b strings.Builder
	for i := 0; i < len(g); i++ {
		c := g[i]
		switch {
		case c == '*' && strings.HasPrefix(g[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case c == '*' && strings.HasPrefix(g[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			j := strings.IndexByte(g[i+1:], ']')
			if j < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := g[i+1 : i+1+j]
			if strings.HasPrefix(class, "!") {
				class = "^" + class[1:]
			}
			b.WriteString("[" + strings.ReplaceAll(class, `\`, `\\`) + "]")
			i += j + 1
		case c == '\\' && i+1 < len(g):
			i++
			b.WriteString(regexp.QuoteMeta(string(g[i])))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// Match reports whether the slash-separated vault-relative path is ignored,
// either itself or through one of its parent directories.
func (m *Ignore) Match(rel string, isDir bool) bool {
	if m == nil || rel == "" || rel == "." {
		return false
	}
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		if m.match(strings.Join(parts[:i], "/"), true) {
			return true
		}
	}
	return m.match(rel, isDir)
}

// match checks one path without looking at its parents.
func (m *Ignore) match(rel string, isDir bool) bool {
	ignored := false
	for _, r := range m.rules {
		if r.dirOnly && !isDir {
			continue
		}
		if r.re.MatchString(rel) {
			ignored = !r.negate
		}
	}
	return ignored
}
