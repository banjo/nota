// Package views loads saved queries from <vault>/.nota/views/*.yaml and
// evaluates them through the index. A view is a virtual folder:
//
//	name: Drafts
//	icon: lucide:pen-line
//	color: blue
//	where: { status: draft }
//	tag: projekt
//	folder: Projekt
//	sort: updated desc
//	limit: 50
//
// Every field except name is optional. icon is one of package icons'
// ("lucide:pen-line", "🏠"; a bare name is Lucide's) and color one of
// icons.Colors. where matches frontmatter values (case-insensitive, list
// items included); tag includes nested tags.
package views

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/banjo/nota/internal/icons"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
)

// View is one saved query.
type View struct {
	// ID is the file name without extension; `nota ls --view ID` uses it.
	ID     string         `yaml:"-" json:"id"`
	Name   string         `yaml:"name" json:"name"`
	Icon   string         `yaml:"icon,omitempty" json:"icon,omitempty"`
	Color  string         `yaml:"color,omitempty" json:"color,omitempty"`
	Where  map[string]any `yaml:"where,omitempty" json:"where,omitempty"`
	Tag    string         `yaml:"tag,omitempty" json:"tag,omitempty"`
	Folder string         `yaml:"folder,omitempty" json:"folder,omitempty"`
	Sort   string         `yaml:"sort,omitempty" json:"sort,omitempty"`
	Limit  int            `yaml:"limit,omitempty" json:"limit,omitempty"`
	// File is the vault-relative path of the definition.
	File string `yaml:"-" json:"file"`
}

// fsDir is the views directory in the vault's FS.
const fsDir = vault.MetaDirName + "/views"

// Load reads every *.yaml and *.yml file in the vault's views directory,
// sorted by name. A missing directory means no views. A broken file fails
// the whole load with an error that names the file.
func Load(v *vault.Vault) ([]View, error) {
	entries, err := v.FS().ReadDir(fsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return []View{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("views: %w", err)
	}
	out := []View{}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		rel := ".nota/views/" + vault.NFC(e.Name())
		data, err := v.FS().ReadFile(fsDir + "/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("views: %w", err)
		}
		vw, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("views: %s: %w", rel, err)
		}
		vw.ID = vault.NFC(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))
		vw.File = rel
		if vw.Name == "" {
			vw.Name = vw.ID
		}
		out = append(out, vw)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

// Parse decodes and validates one view definition.
func Parse(data []byte) (View, error) {
	var vw View
	if err := yaml.UnmarshalWithOptions(data, &vw, yaml.Strict()); err != nil {
		return View{}, err
	}
	if _, err := vw.Query(); err != nil {
		return View{}, err
	}
	return vw, nil
}

// Find returns the view whose ID or name matches name, ignoring case.
func Find(vs []View, name string) (View, bool) {
	name = vault.NFC(strings.TrimSpace(name))
	for _, vw := range vs {
		if strings.EqualFold(vw.ID, name) || strings.EqualFold(vw.Name, name) {
			return vw, true
		}
	}
	return View{}, false
}

// Query turns the view into an index query.
func (vw View) Query() (index.Query, error) {
	srt, err := index.ParseSort(vw.Sort)
	if err != nil {
		return index.Query{}, err
	}
	q := index.Query{Folder: vw.Folder, Tag: vw.Tag, Sort: srt, Limit: vw.Limit}
	keys := make([]string, 0, len(vw.Where))
	for k := range vw.Where {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s, ok := scalar(vw.Where[k])
		if !ok {
			return index.Query{}, fmt.Errorf("where.%s: value must be a string, number or boolean", k)
		}
		q.Where = append(q.Where, index.Cond{Key: k, Value: s})
	}
	return q, nil
}

func scalar(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case int, int64, uint64, uint, int32, uint32:
		return fmt.Sprint(x), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	}
	return "", false
}

// Eval runs the view against the index.
func Eval(ix *index.Index, vw View) ([]index.Entry, error) {
	q, err := vw.Query()
	if err != nil {
		return nil, fmt.Errorf("views: %s: %w", vw.ID, err)
	}
	return ix.Query(q)
}

// SetIcon writes a view's icon and colour into its file, keeping every
// other line as it is; an empty one is taken out. The icon is checked
// and stored as icons.Normalize has it. It returns the view as it is now.
func SetIcon(v *vault.Vault, id, icon, color string) (View, error) {
	vs, err := Load(v)
	if err != nil {
		return View{}, err
	}
	vw, ok := Find(vs, id)
	if !ok {
		return View{}, fmt.Errorf("no view %q: %w", id, vault.ErrNotFound)
	}
	icon = icons.Normalize(icon)
	if icon != "" {
		if err := icons.Check(icon); err != nil {
			return View{}, vault.Invalid(err)
		}
	}
	if err := icons.CheckColor(color); err != nil {
		return View{}, vault.Invalid(err)
	}
	data, err := v.ReadFile(vw.File)
	if err != nil {
		return View{}, err
	}
	data = setKey(setKey(data, "color", color), "icon", icon)
	nv, err := Parse(data)
	if err != nil {
		return View{}, fmt.Errorf("views: %s: %w", vw.File, err)
	}
	if _, _, err := v.WriteFile(vw.File, data); err != nil {
		return View{}, err
	}
	nv.ID, nv.File = vw.ID, vw.File
	if nv.Name == "" {
		nv.Name = nv.ID
	}
	return nv, nil
}

// setKey sets a top-level "key: value" line, after the name and icon
// lines when it is new, or removes it for an empty value.
func setKey(data []byte, key, value string) []byte {
	nl := "\n"
	if strings.Contains(string(data), "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	line := key + ": " + strconv.Quote(value)
	at, after := -1, -1
	for i, l := range lines {
		l = strings.TrimRight(l, "\r")
		switch {
		case strings.HasPrefix(l, key+":"):
			at = i
		case strings.HasPrefix(l, "name:"), strings.HasPrefix(l, "icon:"):
			after = i
		}
	}
	switch {
	case at >= 0 && value == "":
		lines = append(lines[:at], lines[at+1:]...)
	case at >= 0:
		lines[at] = line
	case value != "":
		lines = slices.Insert(lines, after+1, line)
	}
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	return []byte(strings.Join(lines, nl) + nl)
}
