package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The first-run setup of the desktop app (and "Switch vault…" in its
// settings) asks the user for a vault name, a location and an inbox
// folder. The checks live here, next to the rest of the vault rules,
// and the frontend shows their messages as they are.

// NameKind says what a name is for, which only changes the wording.
type NameKind int

const (
	// VaultName names the vault folder.
	VaultName NameKind = iota
	// InboxName names the inbox folder inside a vault.
	InboxName
)

// ErrName is wrapped by every refused name; the message is for the user.
var ErrName = errors.New("invalid name")

type nameError string

func (e nameError) Error() string { return string(e) }
func (e nameError) Unwrap() error { return ErrName }

// CheckName validates a vault or inbox folder name typed by the user
// and returns it cleaned: NFC-normalised and trimmed. It refuses empty
// names, path separators, characters some file system refuses, a
// leading dot (the folder would be hidden), a trailing dot, names
// Windows reserves, and names over 100 characters. The messages are
// written for the person typing.
func CheckName(raw string, kind NameKind) (string, error) {
	n := strings.TrimSpace(NFC(raw))
	bad := func(msg string) (string, error) { return "", nameError(msg) }
	if n == "" {
		if kind == InboxName {
			return bad("Give the folder a name, or skip to use “Inbox”.")
		}
		return bad("Give the vault a name.")
	}
	if strings.ContainsAny(n, `/\`) {
		if kind == InboxName {
			return bad("Use a single folder name, without slashes.")
		}
		return bad("A name can’t contain slashes. Use Location to choose where it goes.")
	}
	var found []string
	hasControl := false
	for _, r := range n {
		switch {
		case unicode.IsControl(r):
			hasControl = true
		case strings.ContainsRune(`:*?"<>|`, r):
			if q := "“" + string(r) + "”"; !slices.Contains(found, q) {
				found = append(found, q)
			}
		}
	}
	if len(found) > 0 {
		return bad("A name can’t contain " + strings.Join(found, " ") + ".")
	}
	if hasControl {
		return bad("A name can’t contain control characters.")
	}
	if strings.HasPrefix(n, ".") {
		return bad("A name can’t start with a dot, because the folder would be hidden.")
	}
	if strings.HasSuffix(n, ".") {
		return bad("A name can’t end with a dot.")
	}
	// Windows reserves the device name with any extension: CON.txt.
	if stem, _, _ := strings.Cut(n, "."); isReservedWindowsName(stem) {
		return bad("“" + n + "” is reserved by the operating system.")
	}
	if utf8.RuneCountInString(n) > maxNameRunes {
		return bad("That name is too long (100 characters at most).")
	}
	return n, nil
}

// ErrLocation is wrapped by every refused location.
var ErrLocation = errors.New("invalid location")

type locationError string

func (e locationError) Error() string { return string(e) }
func (e locationError) Unwrap() error { return ErrLocation }

// systemDirs are places a vault must not be created in.
var systemDirs = []string{
	"/bin", "/sbin", "/usr", "/etc", "/dev", "/proc", "/sys", "/boot", "/lib", "/lib64",
	"/System", "/Library", "/Applications",
}

// CheckLocation validates the folder a new vault goes into: an
// absolute path (a leading ~ is the home directory) that is not the
// root of a drive or a system folder, and not a file. The folder does
// not have to exist; it is created with the vault. It returns the
// cleaned absolute path.
func CheckLocation(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", locationError("Choose where the vault should be created.")
	}
	p, err := expandHome(p)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		return "", locationError("Use a full path, starting with ~ or /.")
	}
	p = filepath.Clean(p)
	if refusedLocation(p) {
		return "", locationError(fmt.Sprintf("Nota can’t write to “%s”. Pick a folder inside your home folder.", p))
	}
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return "", locationError(fmt.Sprintf("“%s” is a file, not a folder.", p))
	}
	return p, nil
}

// refusedLocation reports roots and system folders, unless the home
// directory is inside them (a user whose home is /root may use it).
func refusedLocation(p string) bool {
	if filepath.Dir(p) == p { // "/" or a drive root
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && within(home, p) {
		return false
	}
	if runtime.GOOS == "windows" {
		for _, d := range []string{`Windows`, `Program Files`, `Program Files (x86)`, `ProgramData`} {
			if within(filepath.Join(filepath.VolumeName(p)+`\`, d), p) {
				return true
			}
		}
		return false
	}
	for _, d := range systemDirs {
		if within(d, p) {
			return true
		}
	}
	return false
}

// within reports whether p is dir or inside it.
func within(dir, p string) bool {
	r, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return r == "." || (r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}

// FolderInfo is what InspectFolder found.
type FolderInfo struct {
	// Path is the cleaned absolute path.
	Path string `json:"path"`
	// Exists says the path exists, IsDir that it is a folder.
	Exists bool `json:"exists"`
	IsDir  bool `json:"isDir"`
	// Empty says the folder has nothing in it at all.
	Empty bool `json:"empty"`
	// Notes counts the .md files in it (subfolders included, hidden
	// folders and .nota left out), Files the other files.
	Notes int `json:"notes"`
	Files int `json:"files"`
	// Truncated says the count stopped at inspectLimit entries.
	Truncated bool `json:"truncated"`
	// Writable says Nota can create its .nota folder in it.
	Writable bool `json:"writable"`
}

// inspectLimit bounds how many entries InspectFolder visits, so
// pointing it at a huge tree stays quick.
const inspectLimit = 50000

// InspectFolder looks at a folder a user picked: whether it exists, is
// empty, how many Markdown notes it holds, and whether Nota can write
// to it. It changes nothing.
func InspectFolder(raw string) (FolderInfo, error) {
	p, err := expandHome(strings.TrimSpace(raw))
	if err != nil {
		return FolderInfo{}, err
	}
	if !filepath.IsAbs(p) {
		return FolderInfo{}, locationError("Use a full path, starting with ~ or /.")
	}
	info := FolderInfo{Path: filepath.Clean(p)}
	fi, err := os.Stat(info.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return info, nil
	case err != nil:
		return info, err
	}
	info.Exists, info.IsDir = true, fi.IsDir()
	if !info.IsDir {
		return info, nil
	}
	es, err := os.ReadDir(info.Path)
	if err != nil {
		return info, fmt.Errorf("Nota can’t read “%s”: %w", info.Path, err)
	}
	info.Empty = len(es) == 0
	n := 0
	_ = filepath.WalkDir(info.Path, func(q string, d fs.DirEntry, err error) error {
		if err != nil {
			return fs.SkipDir
		}
		if q == info.Path {
			return nil
		}
		if n++; n > inspectLimit {
			info.Truncated = true
			return fs.SkipAll
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case strings.HasPrefix(name, "."):
		case IsNote(name):
			info.Notes++
		default:
			info.Files++
		}
		return nil
	})
	info.Writable = writable(info.Path)
	return info, nil
}

// writable reports whether Nota can make its .nota folder in dir: it
// creates it and removes it again. A .nota folder that is already there
// stays, and is tested with a scratch file inside.
func writable(dir string) bool {
	meta := filepath.Join(dir, MetaDirName)
	err := os.Mkdir(meta, 0o755)
	switch {
	case err == nil:
		return os.Remove(meta) == nil
	case errors.Is(err, fs.ErrExist):
		f, err := os.CreateTemp(meta, ".write-test-*")
		if err != nil {
			return false
		}
		f.Close()
		return os.Remove(f.Name()) == nil
	}
	return false
}

// ErrNotEmpty is returned by CreateVaultDir when the folder to create
// exists and has files in it; the user can open it as a vault instead.
var ErrNotEmpty = errors.New("folder is not empty")

// CreateVaultDir makes the folder <location>/<name> for a new vault,
// with the location folder too when it is missing, and returns its
// path. The name is checked like CheckName. A folder that exists and is
// empty is used as it is; one with files in it is refused with an
// error wrapping ErrNotEmpty (nothing is touched), and so is one Nota
// cannot write to.
func CreateVaultDir(location, name string) (string, error) {
	loc, err := CheckLocation(location)
	if err != nil {
		return "", err
	}
	n, err := CheckName(name, VaultName)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(loc, n)
	if es, err := os.ReadDir(dir); err == nil && len(es) > 0 {
		return "", fmt.Errorf("A folder named “%s” already exists here and isn’t empty: %w", n, ErrNotEmpty)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("Nota can’t create “%s”: %w", dir, err)
	}
	if !writable(dir) {
		return "", fmt.Errorf("Nota can’t write to “%s”.", dir)
	}
	return dir, nil
}
