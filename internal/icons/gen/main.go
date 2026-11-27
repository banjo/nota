// Command gen downloads the icon sets and the emoji list that
// internal/icons embeds, and writes them to internal/icons/data:
//
//	go generate ./internal/icons
//
// Each set comes from its @iconify-json package on npm at a pinned
// version, trimmed to the icons and aliases; the emoji come from
// Unicode's emoji-test.txt, without the ones newer than maxEmoji, which
// older systems can't draw.
package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// sets are the icon sets and their @iconify-json versions.
var sets = []struct{ prefix, version string }{
	{"lucide", "1.2.160"},
	{"tabler", "1.2.47"},
	{"simple-icons", "1.2.104"},
}

const (
	emojiURL = "https://unicode.org/Public/emoji/16.0/emoji-test.txt"
	maxEmoji = 15.1
)

func main() {
	out := "data"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, s := range sets {
		if err := set(out, s.prefix, s.version); err != nil {
			log.Fatalf("%s: %v", s.prefix, err)
		}
	}
	if err := emoji(out); err != nil {
		log.Fatalf("emoji: %v", err)
	}
}

func get(url string) ([]byte, error) {
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return io.ReadAll(res.Body)
}

// iconSet is the part of an Iconify JSON set that Nota keeps.
type iconSet struct {
	Prefix  string                     `json:"prefix"`
	Width   int                        `json:"width,omitempty"`
	Height  int                        `json:"height,omitempty"`
	Icons   map[string]json.RawMessage `json:"icons"`
	Aliases map[string]json.RawMessage `json:"aliases,omitempty"`
}

func set(out, prefix, version string) error {
	tgz, err := get(fmt.Sprintf("https://registry.npmjs.org/@iconify-json/%s/-/%s-%s.tgz", prefix, prefix, version))
	if err != nil {
		return err
	}
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err != nil {
			return fmt.Errorf("no icons.json: %w", err)
		}
		if h.Name != "package/icons.json" {
			continue
		}
		var s iconSet
		if err := json.NewDecoder(tr).Decode(&s); err != nil {
			return err
		}
		data, err := json.Marshal(s)
		if err != nil {
			return err
		}
		log.Printf("%s %s: %d icons, %d aliases", prefix, version, len(s.Icons), len(s.Aliases))
		return writeGz(filepath.Join(out, prefix+".json.gz"), data)
	}
}

// emojiLine is "1F600 ; fully-qualified # 😀 E1.0 grinning face".
var emojiLine = regexp.MustCompile(`^[0-9A-F ]+;\s*fully-qualified\s*#\s*(\S+)\s+E(\d+\.\d+)\s+(.+)$`)

// emoji writes "emoji\tname\tgroup" lines in Unicode's order.
func emoji(out string) error {
	txt, err := get(emojiURL)
	if err != nil {
		return err
	}
	var b strings.Builder
	group, n := "", 0
	sc := bufio.NewScanner(bytes.NewReader(txt))
	for sc.Scan() {
		l := sc.Text()
		if g, ok := strings.CutPrefix(l, "# group: "); ok {
			group = g
			continue
		}
		m := emojiLine.FindStringSubmatch(l)
		if m == nil || group == "Component" {
			continue
		}
		if v, _ := strconv.ParseFloat(m[2], 64); v > maxEmoji {
			continue
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\n", m[1], m[3], group)
		n++
	}
	log.Printf("emoji: %d", n)
	return writeGz(filepath.Join(out, "emoji.txt.gz"), []byte(b.String()))
}

func writeGz(name string, data []byte) error {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := zw.Write(data); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(name, buf.Bytes(), 0o644)
}
