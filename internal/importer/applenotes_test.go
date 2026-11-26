package importer

import (
	"strings"
	"testing"
	"time"
)

func TestHTMLToMarkdown(t *testing.T) {
	in := `<div><b><span style="font-size: 24px">Inköp</span></b></div>
<div><br></div>
<div>Det här är <b>fett </b>och <i>kursivt</i>, <strike>struket</strike> och <tt>kod</tt>.</div>
<div>Rad två<br></div>
<div><br></div>
<h2>Lista</h2>
<ul>
<li>Mjölk</li>
<li>Ägg</li>
<ul><li>Bruna</li></ul>
</ul>
<div>Efter listan</div>
<ol><li>Ett</li><li>Två</li></ol>
<ul class="checklist"><li class="checked">Klart</li><li>Kvar</li></ul>
<div><a href="https://example.com/a b">länken</a> och <a href="https://nota.app">https://nota.app</a></div>
<div><img src="data:image/png;base64,iVBORw0KGgo=" style="max-width: 100%"><br></div>
<object><table cellspacing="0"><tbody><tr><td><div>Vecka</div></td><td><div>Mål | delmål</div></td></tr><tr><td><div>48</div></td><td><div>Import</div></td></tr></tbody></table></object>
<blockquote><div>Citat</div></blockquote>
<div>Slut&nbsp;här</div>`
	got := HTMLToMarkdown(in, func(src string) string {
		if strings.HasPrefix(src, "data:") {
			return "![[bild.png]]"
		}
		return ""
	})
	want := `**Inköp**

Det här är **fett** och *kursivt*, ~~struket~~ och ` + "`kod`" + `.
Rad två

## Lista
- Mjölk
- Ägg
    - Bruna

Efter listan
1. Ett
2. Två

- [x] Klart
- [ ] Kvar

[länken](https://example.com/a%20b) och <https://nota.app>
![[bild.png]]

| Vecka | Mål \| delmål |
| --- | --- |
| 48 | Import |

> Citat

Slut här
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAppleNotesFiles(t *testing.T) {
	c := time.Date(2023, 1, 2, 3, 4, 5, 0, time.Local)
	m := time.Date(2024, 1, 2, 3, 4, 5, 0, time.Local)
	files := AppleNotesFiles([]AppleNote{
		{Name: "Inköp: vecka 1", Folder: "iCloud/Hem", Body: `<div><h1>Inköp: vecka 1</h1></div><div>Mjölk</div><div><img src="data:image/jpeg;base64,/9j/4AAQ"></div><div><img src="data:image/jpeg;base64,/9j/4AAR"></div>`, Created: c, Modified: m, Attachments: 3},
		{Name: "Inköp: vecka 1", Folder: "iCloud/Hem/Gammalt", Body: `<div>Inköp: vecka 1</div><div><img src="data:image/png;base64,iVBORw0KGgo="></div>`, Created: c, Modified: m, Attachments: 1},
		{Name: "Hemlis", Folder: "iCloud", Locked: true},
	})
	var got []string
	for _, f := range files {
		got = append(got, f.Path+" "+f.Skip+f.Detail)
	}
	want := []string{
		"iCloud/Hem/Inköp vecka 1.md 1 attachment left in Apple Notes",
		"Attachments/Inköp vecka 1 1.jpg ",
		"Attachments/Inköp vecka 1 2.jpg ",
		"iCloud/Hem/Gammalt/Inköp vecka 1.md ",
		"Attachments/Inköp vecka 1 1.png ",
		"iCloud/Hemlis.md the note is locked in Apple Notes",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	b, _ := files[0].Read()
	if string(b) != "# Inköp: vecka 1\nMjölk\n![[Inköp vecka 1 1.jpg]]\n![[Inköp vecka 1 2.jpg]]\n" {
		t.Errorf("note:\n%s", b)
	}
	b, _ = files[3].Read()
	if !strings.HasPrefix(string(b), "# Inköp: vecka 1\n![[Inköp vecka 1 1.png]]") {
		t.Errorf("second note:\n%s", b)
	}

	v := testVault(t, nil)
	r, err := Run(v, files, Options{Folder: "Apple Notes"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Notes != 2 || r.Files != 3 || r.Skipped != 1 {
		t.Errorf("report %+v", r)
	}
	note := read(t, v, "Apple Notes/iCloud/Hem/Inköp vecka 1.md")
	if !strings.HasPrefix(note, "---\ncreated: "+c.Format(time.RFC3339)+"\nupdated: "+m.Format(time.RFC3339)+"\n---\n# Inköp: vecka 1\n") {
		t.Errorf("imported note:\n%s", note)
	}
	if !v.FileExists("Apple Notes/Attachments/Inköp vecka 1 1.png") {
		t.Error("no image")
	}
}

// A note as Notes 15 hands it out: its title, a tag, is not in the body,
// and the image is bold.
func TestAppleNotesTagTitle(t *testing.T) {
	body := `<div><b><img style="max-width: 100%; max-height: 100%;" src="data:image/gif;base64,R0lGODlhAQABAAAAACw="/><br></b><font face=".AppleSystemUIFont"><span style="font-size: 13px"><h1><br></h1></span></font></div>
<div><object><table cellspacing="0" cellpadding="0" style="border-collapse: collapse; direction: ltr">
<tbody>
<tr><td valign="top"><div>Efaef</div>
</td><td valign="top"><br></td></tr>
</tbody>
</table></object><br></div>
<div>Effea</div>
<div><br></div>
<ul>
<li>Feaf</li>
</ul>
<div><br></div>
`
	files := AppleNotesFiles([]AppleNote{{Name: "#Logbook", Folder: "Notes", Body: body, Attachments: 1}})
	if len(files) != 2 || files[0].Path != "Notes/Logbook.md" || files[0].Detail != "" {
		t.Fatalf("files %+v", files)
	}
	b, _ := files[0].Read()
	want := "# #Logbook\n\n![[Logbook 1.gif]]\n\n| Efaef |  |\n| --- | --- |\n\nEffea\n\n- Feaf\n"
	if string(b) != want {
		t.Errorf("got:\n%q\nwant:\n%q", b, want)
	}
}
