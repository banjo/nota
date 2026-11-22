// Command desktop is the Nota desktop app. It serves the React frontend in
// the system webview and gives it the vault through bound Go services: the
// frontend never reads files itself.
package main

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
	"github.com/egoist/mygo"
)

// Note is one markdown file in the vault.
type Note struct {
	Path  string   `json:"path"`
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
	Body  string   `json:"body"`
}

// Vault exposes the notes of one folder to the frontend.
type Vault struct{ dir string }

// List returns every note in the vault, sorted by path.
func (v Vault) List() ([]Note, error) {
	var out []Note
	err := filepath.WalkDir(v.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != v.dir {
			return filepath.SkipDir
		}
		if d.IsDir() || filepath.Ext(p) != ".md" {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		doc := frontmatter.Split(src)
		props, _ := doc.Properties()
		rel, _ := filepath.Rel(v.dir, p)
		n := Note{Path: filepath.ToSlash(rel), Body: string(doc.Body()), Title: titleOf(rel, doc.Body())}
		if tags, ok := props["tags"].([]any); ok {
			for _, t := range tags {
				if s, ok := t.(string); ok {
					n.Tags = append(n.Tags, s)
				}
			}
		}
		out = append(out, n)
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

// Save writes a note's body back, keeping its frontmatter byte for byte.
func (v Vault) Save(path, body string) error {
	p := filepath.Join(v.dir, filepath.FromSlash(path))
	if !strings.HasPrefix(p, filepath.Clean(v.dir)+string(filepath.Separator)) {
		return errors.New("path outside the vault")
	}
	src, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	doc := frontmatter.Split(src)
	head := src[:len(src)-len(doc.Body())]
	return os.WriteFile(p, append(append([]byte{}, head...), body...), 0o644)
}

// Search runs a full-text query over the vault, using the persistent
// index in .nota/ (brought up to date first).
func (v Vault) Search(q string) ([]string, error) {
	vlt, err := vault.Open(v.dir, vault.Options{})
	if err != nil {
		return nil, err
	}
	ix, err := index.OpenVault(vlt)
	if err != nil {
		return nil, err
	}
	defer ix.Close()
	if _, err := ix.Sync(vlt); err != nil {
		return nil, err
	}
	es, err := ix.Search(q, index.Query{})
	if err != nil {
		return nil, err
	}
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Path
	}
	return out, nil
}

// ensureVault creates the vault with a welcome note the first time, so a
// fresh install (or a phone, which has no NOTA_VAULT) opens on something.
func ensureVault(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	welcome := "---\ntags: [nota]\n---\n# Välkommen till Nota\n\nAnteckningarna är vanliga .md-filer i den här mappen.\n"
	return os.WriteFile(filepath.Join(dir, "Välkommen.md"), []byte(welcome), 0o644)
}

// titleOf takes the first line of the body, without a leading "# ".
func titleOf(rel string, body []byte) string {
	for _, l := range strings.Split(string(body), "\n") {
		if l = strings.TrimSpace(strings.TrimLeft(l, "#")); l != "" {
			return l
		}
	}
	return strings.TrimSuffix(filepath.Base(rel), ".md")
}

func main() {
	dir := os.Getenv("NOTA_VAULT")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Notes")
	}
	if err := ensureVault(dir); err != nil {
		log.Fatal(err)
	}
	mygo.Bind(Vault{dir: dir})
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:     "Nota",
			Width:     1200,
			Height:    780,
			MinWidth:  640,
			MinHeight: 420,
			StateKey:  "main",
			URL:       "/",
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
