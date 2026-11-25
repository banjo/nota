package settings

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SchemaURL is where the landing site serves Schema (from
// apps/landing/public/settings.schema.json).
const SchemaURL = "https://nota.banjoanton.com/settings.schema.json"

// Schema returns the JSON Schema of settings.toml, for editors that
// complete TOML (Taplo, Even Better TOML) through a `#:schema` line.
func Schema() []byte {
	props := map[string]any{}
	for _, k := range Keys {
		p := map[string]any{"type": "string", "description": k.Doc, "default": k.Default}
		if k.Values != nil {
			p["enum"] = k.Values
		} else if k.Pattern != nil {
			p["pattern"] = k.Pattern.String()
		}
		props[k.Name] = p
	}
	s := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"$id":         SchemaURL,
		"title":       "Nota settings",
		"description": "~/.config/nota/settings.toml. A key set here wins over the app and the account, and the app does not change it.",
		"type":        "object",
		"properties":  props,
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		panic(err) // plain maps and strings
	}
	return append(data, '\n')
}

// Template is a new settings.toml: the schema line and every key,
// commented out with its default, so uncommenting one sets it.
func Template() string {
	var b strings.Builder
	fmt.Fprintf(&b, "#:schema %s\n", SchemaURL)
	b.WriteString("# Nota settings. A key set here wins over Settings in the app and over\n")
	b.WriteString("# your account, and the app does not change it. Remove a line to let the\n")
	b.WriteString("# app decide again.\n")
	for _, k := range Keys {
		fmt.Fprintf(&b, "\n# %s", k.Doc)
		if k.Values != nil {
			fmt.Fprintf(&b, " One of: %s.", strings.Join(k.Values, ", "))
		}
		fmt.Fprintf(&b, "\n# %s = %q\n", k.Name, k.Default)
	}
	return b.String()
}

// Encode writes values as settings.toml content, in the order of Keys,
// under the schema line.
func Encode(v Values) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#:schema %s\n", SchemaURL)
	for _, k := range Keys {
		if val, ok := v[k.Name]; ok {
			fmt.Fprintf(&b, "%s = %q\n", k.Name, val)
		}
	}
	return b.String()
}
