package server

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/banjo/nota/internal/protocol"
)

func init() { shareIterations = 1000 }

// testWord is a made-up password for a test share.
func testWord(s string) string { return s }

// reader is someone with a link: a plain browser, no account.
type reader struct {
	t      *testing.T
	client *http.Client
}

func newReader(t *testing.T) *reader {
	jar, _ := cookiejar.New(nil)
	return &reader{t: t, client: &http.Client{Jar: jar}}
}

func (rd *reader) get(u string) (int, string, http.Header) {
	rd.t.Helper()
	res, err := rd.client.Get(u)
	if err != nil {
		rd.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b), res.Header
}

func (rd *reader) unlock(u, pw string) (int, string) {
	rd.t.Helper()
	res, err := rd.client.PostForm(u, url.Values{"password": {pw}})
	if err != nil {
		rd.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestShares(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen})
	b, tok := webUser(t, e, "anton@example.com")
	for p, c := range map[string]string{
		"Projekt/Plan.md":         "---\nsecret: yes\n---\n# Plan\n\nSe [[Mål]] och [[Privat]].\n\n![[bild.png]]\n![[hemlig.png|hemlig]]\n",
		"Projekt/Mål.md":          "# Mål\n",
		"Privat.md":               "# Privat\n",
		"Attachments/bild.png":    "\x89PNG bild",
		"Attachments/annan.png":   "\x89PNG annan",
		"Attachments/hemlig.png2": "x",
	} {
		if st, _, perr := e.put(tok, p, 0, c); st != 200 {
			t.Fatalf("put %s: %d %v", p, st, perr)
		}
	}

	// Sharing a note that doesn't exist, or with a theme that isn't one.
	if st, _ := b.call("PUT", api+"/shares/Nope.md", protocol.ShareRequest{}, nil); st != 404 {
		t.Errorf("share missing note: %d", st)
	}
	bad := protocol.ShareRequest{Theme: &protocol.ShareTheme{Tokens: map[string]string{"background": "url(x)"}}}
	if st, _ := b.call("PUT", api+"/shares/Projekt/Plan.md", bad, nil); st != 400 {
		t.Errorf("bad theme: %d", st)
	}

	var plan protocol.Share
	theme := &protocol.ShareTheme{Mode: "dark", Tokens: map[string]string{"ink": "#ff8800"}}
	if st, m := b.call("PUT", api+"/shares/Projekt/Plan.md", protocol.ShareRequest{Theme: theme}, &plan); st != 200 || plan.ID == "" || plan.Password {
		t.Fatalf("share: %d %v %+v", st, m, plan)
	}
	if !strings.HasSuffix(plan.URL, "/s/"+plan.ID) {
		t.Errorf("url %s", plan.URL)
	}
	// Sharing again keeps the link.
	var again protocol.Share
	b.call("PUT", api+"/shares/Projekt/Plan.md", protocol.ShareRequest{}, &again)
	if again.ID != plan.ID || again.Theme == nil || again.Theme.Tokens["ink"] != "#ff8800" {
		t.Errorf("share again: %+v", again)
	}

	rd := newReader(t)
	st, page, hdr := rd.get(plan.URL)
	if st != 200 {
		t.Fatalf("page: %d %s", st, page)
	}
	for _, want := range []string{`<h1 class="title">Plan</h1>`, `data-mode="dark"`, `--ink:#ff8800;`,
		`<span class="unresolved">Mål</span>`, `<span class="unresolved">Privat</span>`,
		`<img src="/s/` + plan.ID + `/f/Attachments/bild.png"`} {
		if !strings.Contains(page, want) {
			t.Errorf("page: missing %s", want)
		}
	}
	if strings.Contains(page, "secret") {
		t.Error("page shows the frontmatter")
	}
	if !strings.Contains(hdr.Get("Content-Security-Policy"), "default-src 'none'") || hdr.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("headers %v", hdr)
	}

	// A linked note that is shared too becomes a link.
	var mal protocol.Share
	b.call("PUT", api+"/shares/Projekt/Mål.md", protocol.ShareRequest{}, &mal)
	if _, page, _ = rd.get(plan.URL); !strings.Contains(page, `<a href="/s/`+mal.ID+`" class="internal">Mål</a>`) {
		t.Errorf("shared link: %s", page)
	}

	// Images: the ones the note shows, nothing else of the vault.
	base := e.ts.URL + "/s/" + plan.ID + "/f/"
	if st, body, h := rd.get(base + "Attachments/bild.png"); st != 200 || body != "\x89PNG bild" || h.Get("Content-Type") != "image/png" {
		t.Errorf("image: %d %q %v", st, body, h)
	}
	for _, p := range []string{"Attachments/annan.png", "Privat.md", "Attachments/hemlig.png2"} {
		if st, _, _ := rd.get(base + p); st != 404 {
			t.Errorf("%s: %d, want 404", p, st)
		}
	}

	// The page is live.
	var n Note
	b.call("GET", notePathURL("Projekt/Plan.md"), nil, &n)
	if st, m := b.callBase("PUT", notePathURL("Projekt/Plan.md"), n.Version, map[string]string{"content": "# Plan\n\nNy text.\n\n![[bild.png]]\n"}, nil); st != 200 {
		t.Fatalf("save: %d %v", st, m)
	}
	if _, page, _ = rd.get(plan.URL); !strings.Contains(page, "Ny text.") {
		t.Error("page not live")
	}

	// A move keeps the link.
	if st, m := b.call("POST", api+"/move", MoveRequest{Path: "Projekt/Plan.md", To: "Arkiv/Plan.md"}, nil); st != 200 {
		t.Fatalf("move: %d %v", st, m)
	}
	var list []protocol.Share
	b.call("GET", api+"/shares", nil, &list)
	if len(list) != 2 || list[0].Path != "Arkiv/Plan.md" || list[0].ID != plan.ID {
		t.Fatalf("after move: %+v", list)
	}
	if st, _, _ := rd.get(plan.URL); st != 200 {
		t.Errorf("moved page: %d", st)
	}

	// A password: the page asks for it, a wrong one is refused, the right
	// one opens the note and its images.
	pw := testWord("hemligt ord")
	if st, _ := b.call("PUT", api+"/shares/Arkiv/Plan.md", protocol.ShareRequest{Password: &pw}, &plan); st != 200 || !plan.Password {
		t.Fatalf("set password: %d %+v", st, plan)
	}
	rd = newReader(t)
	if st, page, _ := rd.get(plan.URL); st != 401 || !strings.Contains(page, "This note is protected") || strings.Contains(page, "Ny text") {
		t.Fatalf("locked: %d %s", st, page)
	}
	if st, _, _ := rd.get(base + "Attachments/bild.png"); st != 404 {
		t.Errorf("locked image: %d", st)
	}
	if st, page := rd.unlock(plan.URL, "fel"); st != 401 || !strings.Contains(page, "That password") {
		t.Errorf("wrong password: %d", st)
	}
	if st, page := rd.unlock(plan.URL, pw); st != 200 || !strings.Contains(page, "Ny text.") {
		t.Fatalf("unlock: %d %s", st, page)
	}
	if st, _, _ := rd.get(base + "Attachments/bild.png"); st != 200 {
		t.Errorf("unlocked image: %d", st)
	}
	// A new password locks the reader out again.
	second := testWord("nytt")
	b.call("PUT", api+"/shares/Arkiv/Plan.md", protocol.ShareRequest{Password: &second}, nil)
	if st, _, _ := rd.get(plan.URL); st != 401 {
		t.Errorf("after new password: %d", st)
	}
	none := ""
	b.call("PUT", api+"/shares/Arkiv/Plan.md", protocol.ShareRequest{Password: &none}, &plan)
	if st, _, _ := newReader(t).get(plan.URL); st != 200 || plan.Password {
		t.Errorf("password removed: %d", st)
	}

	// Unsharing: the link leads nowhere.
	if st, _ := b.call("DELETE", api+"/shares/Arkiv/Plan.md", nil, nil); st != 204 {
		t.Fatalf("unshare: %d", st)
	}
	if st, page, _ := rd.get(plan.URL); st != 404 || !strings.Contains(page, "doesn't lead anywhere") {
		t.Errorf("unshared: %d", st)
	}
	if st, _ := b.call("DELETE", api+"/shares/Arkiv/Plan.md", nil, nil); st != 404 {
		t.Errorf("unshare twice: %d", st)
	}

	// Deleting a note unshares it.
	if st, _ := b.call("DELETE", notePathURL("Projekt/Mål.md"), nil, nil); st/100 != 2 {
		t.Fatalf("delete: %d", st)
	}
	if b.call("GET", api+"/shares", nil, &list); len(list) != 0 {
		t.Errorf("after delete: %+v", list)
	}

	// Another user sees none of it.
	b2, _ := webUser(t, e, "eva@example.com")
	if st, _ := b2.call("GET", api+"/shares", nil, &list); st != 200 || len(list) != 0 {
		t.Errorf("other user: %d %+v", st, list)
	}
}

func TestShareRateLimit(t *testing.T) {
	e, _ := authEnv(t, Config{Signup: SignupOpen, AuthRateLimit: 30})
	b, tok := webUser(t, e, "anton@example.com")
	e.put(tok, "Plan.md", 0, "# Plan\n")
	pw := testWord("rätt")
	var sh protocol.Share
	b.call("PUT", api+"/shares/Plan.md", protocol.ShareRequest{Password: &pw}, &sh)
	rd := newReader(t)
	limited := false
	for range 15 {
		if st, _ := rd.unlock(sh.URL, "fel"); st == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("never rate limited")
	}
}

func TestSharePassword(t *testing.T) {
	h, err := hashSharePassword("åäö")
	if err != nil {
		t.Fatal(err)
	}
	if !checkSharePassword(h, "åäö") || checkSharePassword(h, "aao") || checkSharePassword("", "") {
		t.Fatal("check")
	}
}
