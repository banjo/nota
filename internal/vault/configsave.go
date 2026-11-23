package vault

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// SaveConfig writes the keys of c that are set (vault, inbox, onboarded)
// into the config file at path, creating it and its directory when
// needed. Everything else in the file stays as it is: unknown keys,
// tables, comments and the order of the lines. The write is atomic (a
// temp file in the same directory, then a rename), so a crash never
// leaves half a config behind.
func SaveConfig(path string, c Config) error {
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("vault: read config %s: %w", path, err)
	}
	var upd []topLevel
	for _, kv := range []struct {
		key string
		val any
		set bool
	}{
		{"vault", c.Vault, c.Vault != ""},
		{"inbox", c.Inbox, c.Inbox != ""},
		{"onboarded", c.Onboarded, c.Onboarded},
	} {
		if !kv.set {
			continue
		}
		var b bytes.Buffer
		if err := toml.NewEncoder(&b).Encode(map[string]any{kv.key: kv.val}); err != nil {
			return fmt.Errorf("vault: encode config: %w", err)
		}
		upd = append(upd, topLevel{kv.key, strings.TrimRight(b.String(), "\n")})
	}
	out := setTopLevel(string(old), upd)
	// Never write something the next start could not read.
	var probe map[string]any
	if _, err := toml.Decode(out, &probe); err != nil {
		return fmt.Errorf("vault: %s is not valid TOML, not changing it: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("vault: write config: %w", err)
	}
	return writeAtomic(path, []byte(out))
}

// topLevel is one `key = value` line for the top of a config file.
type topLevel struct{ key, line string }

// setTopLevel replaces the top-level assignments of the keys in upd and
// adds the ones that are missing after the last top-level line (before
// the first table). Lines it does not touch are kept byte for byte.
func setTopLevel(src string, upd []topLevel) string {
	lines := strings.SplitAfter(src, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	eol := "\n"
	if strings.Contains(src, "\r\n") {
		eol = "\r\n"
	}
	// The top-level part ends at the first table header outside a
	// multi-line string.
	end, inMulti := len(lines), ""
	for i, l := range lines {
		if inMulti != "" {
			if strings.Count(l, inMulti)%2 == 1 {
				inMulti = ""
			}
			continue
		}
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			end = i
			break
		}
		for _, q := range []string{`"""`, `'''`} {
			if strings.Count(l, q)%2 == 1 {
				inMulti = q
			}
		}
	}
	done := map[string]bool{}
	for i := 0; i < end; i++ {
		key, ok := assignedKey(lines[i])
		if !ok {
			continue
		}
		for _, u := range upd {
			if u.key == key && !done[key] {
				lines[i] = u.line + eol
				done[key] = true
			}
		}
	}
	var add []string
	for _, u := range upd {
		if !done[u.key] {
			add = append(add, u.line+eol)
		}
	}
	if len(add) == 0 {
		return strings.Join(lines, "")
	}
	// Insert after the last non-blank line of the top-level part.
	at := end
	for at > 0 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	if at > 0 && !strings.HasSuffix(lines[at-1], "\n") {
		lines[at-1] += eol
	}
	rest := append([]string{}, lines[at:]...)
	out := append(append(append([]string{}, lines[:at]...), add...), rest...)
	return strings.Join(out, "")
}

// assignedKey returns the bare key of a `key = value` line.
func assignedKey(line string) (string, bool) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", false
	}
	k, _, ok := strings.Cut(t, "=")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(k), true
}

// NeedsOnboarding reports whether this is a first run of the desktop
// app, which should ask where to keep the notes. It is when nothing
// chose a vault (no flag, no $NOTA_VAULT, no `vault` key in the config:
// loc.Source is "default"), setup has not finished before, and ~/Notes
// is missing or empty. Anyone with the env var, a config or notes in
// ~/Notes never sees it. When the state cannot be read it says no, so
// an unreadable folder is reported by opening it, not hidden by a
// wizard.
func NeedsOnboarding(loc Location) (bool, error) {
	if loc.Source != "default" || loc.Config.Onboarded {
		return false, nil
	}
	es, err := os.ReadDir(loc.Dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true, nil
	case err != nil:
		return false, fmt.Errorf("vault: look at %s: %w", loc.Dir, err)
	}
	return len(es) == 0, nil
}
