// Package settings is the user's settings: choices that follow the user
// to every machine, such as the theme. They come in layers, and the
// highest layer that sets a key wins:
//
//	file     ~/.config/nota/settings.toml, written by hand (dotfiles); the app never writes it
//	account  the sync server, per user; what the app writes when signed in
//	device   the app's own storage on this machine (the frontend's localStorage)
//	default  Defaults
//
// A key set in the file is locked: the app shows it but does not change
// it. Keys and values are described once, in Keys, which also produces
// the JSON Schema editors use for completion (Schema).
package settings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Key describes one setting.
type Key struct {
	// Name is the key in settings.toml and the API, e.g. "note_mode".
	Name string
	// Doc says what the key does, for the schema and `nota settings`.
	Doc string
	// Default is the value when no layer sets the key.
	Default string
	// Values are the allowed values; nil allows any value Pattern matches.
	Values []string
	// Pattern checks values when Values is nil.
	Pattern *regexp.Regexp
}

// Valid reports whether v is a value the key takes.
func (k Key) Valid(v string) bool {
	if k.Values != nil {
		return slices.Contains(k.Values, v)
	}
	return k.Pattern != nil && k.Pattern.MatchString(v)
}

// paletteID is a colour theme id, as the frontend checks it.
var paletteID = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*$`)

// Keys are the known settings, in display order.
var Keys = []Key{
	{Name: "theme", Doc: "Light or dark mode: system follows the operating system.",
		Default: "system", Values: []string{"system", "light", "dark"}},
	{Name: "palette", Doc: "The colour theme, by id (nota, paper, …; a plugin can add more).",
		Default: "nota", Pattern: paletteID},
	{Name: "note_mode", Doc: "How notes open: live (editing with formatting), reading or source.",
		Default: "live", Values: []string{"live", "reading", "source"}},
	{Name: "properties", Doc: "Whether a note's properties (frontmatter) show above it.",
		Default: "hide", Values: []string{"hide", "show"}},
}

// Lookup returns the key named name.
func Lookup(name string) (Key, bool) {
	for _, k := range Keys {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

// Names returns the names of Keys.
func Names() []string {
	out := make([]string, len(Keys))
	for i, k := range Keys {
		out[i] = k.Name
	}
	return out
}

// Values are the settings one layer sets, by key. A key that is absent
// is not set in that layer.
type Values map[string]string

// Defaults returns every key with its default value.
func Defaults() Values {
	out := Values{}
	for _, k := range Keys {
		out[k.Name] = k.Default
	}
	return out
}

// Check keeps the known keys with valid values and describes the rest.
// Unknown keys and bad values are warnings, not errors: a file written
// for a newer Nota must not break an older one, and a bad value falls
// back to the layer below.
func Check(v Values) (Values, []string) {
	out := Values{}
	var warnings []string
	names := make([]string, 0, len(v))
	for name := range v {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		val := v[name]
		k, ok := Lookup(name)
		switch {
		case !ok:
			warnings = append(warnings, fmt.Sprintf("unknown setting %q is ignored", name))
		case !k.Valid(val):
			warnings = append(warnings, fmt.Sprintf("%s = %q is not valid (%s); it is ignored", name, val, k.describe()))
		default:
			out[name] = val
		}
	}
	return out, warnings
}

func (k Key) describe() string {
	if k.Values != nil {
		return "want " + strings.Join(k.Values, ", ")
	}
	return "want an id like " + k.Default
}

// The account layer stores keys this Nota may not know yet (a newer
// client wrote them), so the server only checks their shape.
const (
	MaxKeys     = 64
	MaxValueLen = 256
)

var keyName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ValidName reports whether name can be a setting key: lower case
// letters, digits and underscores, starting with a letter.
func ValidName(name string) bool { return keyName.MatchString(name) }

// File is the settings file name in the config directory.
const File = "settings.toml"

// Path returns where the settings file is: $XDG_CONFIG_HOME/nota/settings.toml,
// or ~/.config/nota/settings.toml, on every OS (next to config.toml).
func Path() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "nota", File), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("settings: find home directory: %w", err)
	}
	return filepath.Join(home, ".config", "nota", File), nil
}

// Parse reads settings.toml. Only a file that is not TOML is an error;
// unknown keys, values of the wrong type and invalid values are left out
// with a warning each.
func Parse(data []byte) (Values, []string, error) {
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, nil, err
	}
	v := Values{}
	var warnings []string
	for name, val := range raw {
		s, ok := val.(string)
		if !ok {
			if _, known := Lookup(name); known {
				warnings = append(warnings, fmt.Sprintf("%s must be a string; it is ignored", name))
			} else {
				warnings = append(warnings, fmt.Sprintf("unknown setting %q is ignored", name))
			}
			continue
		}
		v[name] = s
	}
	out, more := Check(v)
	warnings = append(warnings, more...)
	sort.Strings(warnings)
	return out, warnings, nil
}

// Load reads the settings file at path. A missing file sets nothing and
// is not an error.
func Load(path string) (Values, []string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Values{}, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("settings: %w", err)
	}
	v, warnings, err := Parse(data)
	if err != nil {
		return nil, nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	return v, warnings, nil
}

// Source says which layer a value comes from.
type Source string

// Sources, lowest first.
const (
	SourceDefault Source = "default"
	SourceDevice  Source = "device"
	SourceAccount Source = "account"
	SourceFile    Source = "file"
)

// Layer is the values of one source.
type Layer struct {
	Source Source
	Values Values
}

// Value is a resolved setting and where it came from.
type Value struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source Source `json:"source"`
}

// Merge resolves every key: the value of the last layer that sets it,
// else the default. Layers go lowest first (device, account, file);
// invalid values in them are skipped.
func Merge(layers ...Layer) []Value {
	out := make([]Value, len(Keys))
	for i, k := range Keys {
		out[i] = Value{Key: k.Name, Value: k.Default, Source: SourceDefault}
		for _, l := range layers {
			if v, ok := l.Values[k.Name]; ok && k.Valid(v) {
				out[i].Value, out[i].Source = v, l.Source
			}
		}
	}
	return out
}
