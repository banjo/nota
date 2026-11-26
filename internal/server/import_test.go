package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/banjo/nota/internal/importer"
)

func TestImport(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen, MaxFileSize: 1 << 20})
	b, tok := webUser(t, e, "anton@example.com")
	if st, _, perr := e.put(tok, "Bear/Plan.md", 0, "# Old plan\n"); st != 200 {
		t.Fatalf("put: %d %v", st, perr)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, c := range map[string]string{
		"Bear/Plan.md":         "# Plan\n\nSee [[Möte]].\n",
		"Bear/Möte.md":         "# Möte\n\nBack to [[Plan]].\n",
		"Bear/Stor.png":        string(make([]byte, 2<<20)),
		"Bear/.hidden/x.md":    "x",
		"__MACOSX/Bear/._x.md": "x",
	} {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	zipped := buf.String()

	post := func(query string) (int, importer.Report) {
		t.Helper()
		res, body := b.raw("POST", api+"/import"+query, zipped, map[string]string{"Nota-Protocol": "1", "Content-Type": "application/zip"})
		var r importer.Report
		if res.StatusCode == 200 {
			if err := json.Unmarshal([]byte(body), &r); err != nil {
				t.Fatal(err)
			}
		} else {
			t.Logf("%s: %s", query, body)
		}
		return res.StatusCode, r
	}

	st, dry := post("?dryRun=1")
	if st != 200 || !dry.DryRun || dry.Folder != "Bear 2" || dry.Notes != 2 || dry.Skipped != 1 {
		t.Fatalf("dry run: %d %+v", st, dry)
	}

	// Into the folder that has a Plan already: it becomes "Plan 2", and
	// Möte's link follows it.
	st, r := post("?folder=Bear&name=Bear.zip")
	if st != 200 || r.Folder != "Bear" || r.Notes != 2 || r.Renamed != 1 || r.Skipped != 1 || r.Links != 1 {
		t.Fatalf("import: %d %+v", st, r)
	}
	var n Note
	if st, m := b.call("GET", api+"/notes/Bear/Möte.md", nil, &n); st != 200 {
		t.Fatalf("get: %d %v", st, m)
	}
	if n.Body != "# Möte\n\nBack to [[Plan 2|Plan]].\n" || n.Version == 0 {
		t.Errorf("Möte: %+v", n)
	}
	var tree Tree
	if st, m := b.call("GET", api+"/tree", nil, &tree); st != 200 || len(tree.Notes) != 3 {
		t.Errorf("tree: %d %v %+v", st, m, tree.Notes)
	}

	if res, body := b.raw("POST", api+"/import", "not a zip", map[string]string{"Nota-Protocol": "1"}); res.StatusCode != 400 {
		t.Errorf("not a zip: %d %s", res.StatusCode, body)
	}
}
