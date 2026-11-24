// Package plugins loads the plugins of a vault: one folder each in
// <vault>/.nota/plugins, described by its manifest.json. Plugins sync
// with the vault. For now a plugin contributes colour themes only, as
// data; nothing in a plugin runs.
//
//	{
//	  "id": "sunset",
//	  "name": "Sunset",
//	  "version": "1.0.0",
//	  "contributes": {
//	    "themes": [{
//	      "id": "sunset",
//	      "name": "Sunset",
//	      "light": { "background": "#fff8f0", "text": "#3a2a20", "ink": "#c2410c" },
//	      "dark":  { "background": "#1f1612", "text": "#f2e6dc", "ink": "#fb923c" }
//	    }]
//	  }
//	}
//
// A theme sets the app's colour tokens (Tokens) per mode. background,
// text and ink are required in every mode it has; the app derives the
// rest from them. docs/themes.md is the reference.
package plugins

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/banjo/nota/internal/vault"
)

// Dir is the plugins directory in the vault's FS.
const Dir = vault.MetaDirName + "/plugins"

// ManifestName is the file that describes a plugin.
const ManifestName = "manifest.json"

// maxManifest is the largest manifest.json read.
const maxManifest = 256 << 10

// Tokens are the colour tokens a theme may set, as in
// packages/ui/src/tokens.css without the leading "--". The md-* tokens
// colour the Markdown in the editor and fall back to the others. The
// app keeps the same list (packages/app/src/theme/custom.ts).
var Tokens = []string{
	"background", "foreground", "text", "text-soft", "muted-foreground", "faint",
	"sidebar", "hairline", "divider", "hover", "selected", "chip", "chip-foreground",
	"file", "code", "surface", "overlay",
	"ink", "ink-soft", "ink-soft-hover", "ink-strong", "ink-foreground",
	"success", "warning", "danger",
	"md-heading", "md-bold", "md-italic", "md-link", "md-tag", "md-code", "md-quote",
	"md-table-border", "md-table-header",
}

// Required are the tokens every mode of a theme sets, as opaque hex
// colours: the app derives the others from them.
var Required = []string{"background", "text", "ink"}

// Plugin is one folder of .nota/plugins. A plugin that could not be
// read has Error set and nothing else but ID and Dir.
type Plugin struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version,omitempty"`
	Description string      `json:"description,omitempty"`
	Author      string      `json:"author,omitempty"`
	Contributes Contributes `json:"contributes"`
	// Dir is the vault-relative folder of the plugin.
	Dir string `json:"dir"`
	// Error says why the plugin did not load.
	Error string `json:"error,omitempty"`
}

// Contributes is what a plugin adds to the app.
type Contributes struct {
	Themes []Theme `json:"themes"`
}

// Theme is a colour theme. It has a light and a dark variant, or one
// of them: picking a theme with one variant switches the app to it.
type Theme struct {
	ID    string            `json:"id"`
	Name  string            `json:"name"`
	Light map[string]string `json:"light,omitempty"`
	Dark  map[string]string `json:"dark,omitempty"`
}

var (
	idRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	hexRe   = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	opaqRe  = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	colorFn = regexp.MustCompile(`^(rgb|rgba|hsl|hsla)\([0-9.,%/ deg+-]+\)$`)
)

// Load reads every plugin folder in the vault, sorted by id. A missing
// directory means no plugins. A plugin that is broken comes back with
// Error set, so one bad manifest does not hide the others.
func Load(v *vault.Vault) ([]Plugin, error) {
	entries, err := v.FS().ReadDir(Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Plugin{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("plugins: %w", err)
	}
	out := []Plugin{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := vault.NFC(e.Name())
		rel := Dir + "/" + name
		data, err := v.FS().ReadFile(rel + "/" + ManifestName)
		var p Plugin
		switch {
		case errors.Is(err, fs.ErrNotExist):
			p = Plugin{Error: "no " + ManifestName}
		case err != nil:
			p = Plugin{Error: err.Error()}
		case len(data) > maxManifest:
			p = Plugin{Error: fmt.Sprintf("%s is larger than %d KB", ManifestName, maxManifest>>10)}
		default:
			p, err = Parse(data, name)
			if err != nil {
				p = Plugin{Error: err.Error()}
			}
		}
		if p.ID == "" {
			p.ID = name
		}
		p.Dir = rel
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		// The same id twice: the folder named after it keeps it.
		oi, oj := out[i].Dir == Dir+"/"+out[i].ID, out[j].Dir == Dir+"/"+out[j].ID
		if oi != oj {
			return oi
		}
		return out[i].Dir < out[j].Dir
	})
	for i := 1; i < len(out); i++ {
		if out[i].ID == out[i-1].ID && out[i].Error == "" {
			out[i] = Plugin{ID: out[i].ID, Dir: out[i].Dir,
				Error: fmt.Sprintf("the id %q is taken by %s", out[i].ID, out[i-1].Dir)}
		}
	}
	for i := range out {
		if out[i].Contributes.Themes == nil {
			out[i].Contributes.Themes = []Theme{}
		}
	}
	return out, nil
}

// Parse decodes and checks a manifest. dir is the plugin's folder name,
// the id when the manifest has none.
func Parse(data []byte, dir string) (Plugin, error) {
	var p Plugin
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Plugin{}, fmt.Errorf("%s: %w", ManifestName, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Plugin{}, fmt.Errorf("%s: more than one JSON value", ManifestName)
	}
	if p.ID == "" {
		p.ID = strings.ToLower(dir)
	}
	if !idRe.MatchString(p.ID) {
		return Plugin{}, fmt.Errorf("id %q: use lowercase letters, digits and dashes", p.ID)
	}
	if p.Name == "" {
		p.Name = p.ID
	}
	if p.Contributes.Themes == nil {
		p.Contributes.Themes = []Theme{}
	}
	seen := map[string]bool{}
	for i := range p.Contributes.Themes {
		t := &p.Contributes.Themes[i]
		if err := checkTheme(t); err != nil {
			label := t.ID
			if label == "" {
				label = fmt.Sprint(i + 1)
			}
			return Plugin{}, fmt.Errorf("theme %s: %w", label, err)
		}
		if seen[t.ID] {
			return Plugin{}, fmt.Errorf("theme %s: the id is used twice", t.ID)
		}
		seen[t.ID] = true
	}
	return p, nil
}

func checkTheme(t *Theme) error {
	if !idRe.MatchString(t.ID) {
		return fmt.Errorf("id %q: use lowercase letters, digits and dashes", t.ID)
	}
	if t.Name == "" {
		t.Name = t.ID
	}
	if t.Light == nil && t.Dark == nil {
		return errors.New(`needs "light", "dark" or both`)
	}
	for _, m := range []struct {
		name   string
		tokens map[string]string
	}{{"light", t.Light}, {"dark", t.Dark}} {
		if m.tokens == nil {
			continue
		}
		if err := checkTokens(m.tokens); err != nil {
			return fmt.Errorf("%s: %w", m.name, err)
		}
	}
	return nil
}

func checkTokens(tokens map[string]string) error {
	keys := make([]string, 0, len(tokens))
	for k := range tokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		val := strings.TrimSpace(tokens[k])
		tokens[k] = val
		if !slices.Contains(Tokens, k) {
			return fmt.Errorf("unknown token %q (see docs/themes.md)", k)
		}
		if !IsColor(val) {
			return fmt.Errorf("%s: %q is not a colour; use #rrggbb, rgb() or hsl()", k, val)
		}
	}
	for _, k := range Required {
		val, ok := tokens[k]
		if !ok {
			return fmt.Errorf("%s is required", k)
		}
		if !opaqRe.MatchString(val) {
			return fmt.Errorf("%s: %q must be a hex colour without alpha, like #282a36", k, val)
		}
	}
	return nil
}

// IsColor reports whether s is a colour a theme may use: hex, or
// rgb()/hsl() with numbers. Anything else (url(), var(), keywords) is
// refused, so a synced theme can't do more than colour the app.
func IsColor(s string) bool {
	return hexRe.MatchString(s) || colorFn.MatchString(s)
}
