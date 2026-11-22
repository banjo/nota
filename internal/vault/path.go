package vault

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Ext is the file extension of a note.
const Ext = ".md"

// Errors returned by vault operations. Use errors.Is to test for them.
var (
	ErrNotFound      = errors.New("not found")
	ErrExists        = errors.New("already exists")
	ErrCaseCollision = errors.New("differs only in case from an existing path")
	ErrInvalidPath   = errors.New("invalid path")
	ErrNotNote       = errors.New("not a markdown note")
)

// PathError describes a failed operation on one vault path.
type PathError struct {
	Op   string
	Path string
	// Existing is the path already on disk, for ErrExists and ErrCaseCollision.
	Existing string
	Err      error
}

func (e *PathError) Error() string {
	if e.Existing != "" && e.Existing != e.Path {
		return fmt.Sprintf("%s %s: %v: %s", e.Op, e.Path, e.Err, e.Existing)
	}
	return fmt.Sprintf("%s %s: %v", e.Op, e.Path, e.Err)
}

func (e *PathError) Unwrap() error { return e.Err }

// NFC returns s in Unicode normalization form C. macOS file systems may
// hand out decomposed (NFD) names, Linux and the sync protocol use NFC.
func NFC(s string) string { return norm.NFC.String(s) }

// CleanPath turns user input into a canonical vault-relative path: NFC,
// forward slashes, no "." or ".." segments, no leading slash. It refuses
// paths that leave the vault. The empty string means the vault root.
func CleanPath(p string) (string, error) {
	p = NFC(strings.ReplaceAll(p, `\`, "/"))
	if strings.ContainsRune(p, 0) {
		return "", &PathError{Op: "clean", Path: p, Err: ErrInvalidPath}
	}
	c := path.Clean("/" + p)
	if c == "/" {
		return "", nil
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", &PathError{Op: "clean", Path: p, Err: fmt.Errorf("%w: leaves the vault", ErrInvalidPath)}
		}
	}
	return strings.TrimPrefix(c, "/"), nil
}

// NotePath cleans p and adds ".md" when it has no extension.
func NotePath(p string) (string, error) {
	c, err := CleanPath(p)
	if err != nil {
		return "", err
	}
	if c == "" {
		return "", &PathError{Op: "clean", Path: p, Err: ErrInvalidPath}
	}
	if !strings.EqualFold(path.Ext(c), Ext) {
		c += Ext
	}
	return c, nil
}

// IsNote reports whether a file name is a markdown note.
func IsNote(name string) bool { return strings.EqualFold(path.Ext(name), Ext) }

// maxNameRunes caps generated file names so they fit every file system
// with room for the " 2" suffix and the extension.
const maxNameRunes = 100

// Filename turns a note title into a file name stem: characters that are
// unsafe on some file system or special in wikilinks are replaced, runs of
// white space collapse, and leading dots and trailing dots or spaces are
// removed. Letters such as å, ä and ö are kept. It never returns "".
func Filename(title string) string {
	var b strings.Builder
	space := false
	for _, r := range NFC(title) {
		switch {
		case strings.ContainsRune(`/\:*?"<>|#^[]`, r), unicode.IsControl(r), unicode.IsSpace(r):
			space = b.Len() > 0
			continue
		case r == utf8.RuneError:
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	s := b.String()
	if utf8.RuneCountInString(s) > maxNameRunes {
		s = string([]rune(s)[:maxNameRunes])
	}
	s = strings.TrimLeft(s, ". ")
	s = strings.TrimRight(s, ". ")
	switch {
	case s == "":
		s = "Untitled"
	case isReservedWindowsName(s):
		s += " note"
	}
	return s
}

// isReservedWindowsName reports device names Windows refuses as files.
func isReservedWindowsName(s string) bool {
	switch strings.ToUpper(s) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// foldKey is the key two names collide on in a case-insensitive file system.
func foldKey(name string) string { return strings.ToLower(NFC(name)) }
