package main

import (
	"errors"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	nsync "github.com/banjo/nota/internal/sync"
	"github.com/banjo/nota/internal/vault"
)

// testDesktop is a Desktop that records relaunches and keeps its config
// in the temp dir.
func testDesktop(t *testing.T) (*Desktop, *int, string) {
	t.Helper()
	relaunched := new(int)
	cfg := filepath.Join(t.TempDir(), "nota", "config.toml")
	return &Desktop{onboarding: true, source: "default", configPath: cfg, relaunch: func(...string) { *relaunched++ }}, relaunched, cfg
}

func TestCreateVaultMakesTheFolderAndTheConfig(t *testing.T) {
	d, relaunched, cfg := testDesktop(t)
	loc := filepath.Join(t.TempDir(), "Documents")
	res, err := d.CreateVault(loc, " My Notes ", "Capture", true)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(loc, "My Notes")
	if res.Path != want || res.Name != "My Notes" || *relaunched != 1 {
		t.Fatalf("result %+v, relaunched %d", res, *relaunched)
	}
	c, err := vault.LoadConfig(cfg)
	if err != nil || c.Vault != want || c.Inbox != "Capture" || !c.Onboarded {
		t.Fatalf("config %+v, %v", c, err)
	}
	b, err := os.ReadFile(filepath.Join(want, "Welcome.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"# Welcome to Nota", "into Capture.", "[[double brackets]]"} {
		if !strings.Contains(string(b), s) {
			t.Errorf("welcome note lacks %q:\n%s", s, b)
		}
	}
	if strings.Contains(string(b), "Välkommen") {
		t.Error("the welcome note is still Swedish")
	}
	// The next start finds the vault by the config and no onboarding.
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(filepath.Dir(cfg)))
	t.Setenv(vault.EnvVault, "")
	l, err := vault.Discover("")
	if err != nil || l.Dir != want || l.Source != "config" {
		t.Fatalf("Discover = %+v, %v", l, err)
	}
	if need, _ := vault.NeedsOnboarding(l); need {
		t.Error("asks again after onboarding")
	}
}

func TestCreateVaultWithoutWelcomeAndDefaultInbox(t *testing.T) {
	d, _, cfg := testDesktop(t)
	res, err := d.CreateVault(t.TempDir(), "Notes", "  ", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "Welcome.md")); err == nil {
		t.Error("welcome note written although unchecked")
	}
	if c, _ := vault.LoadConfig(cfg); c.Inbox != "Inbox" {
		t.Errorf("inbox = %q, want the default", c.Inbox)
	}
}

func TestCreateVaultRefusals(t *testing.T) {
	d, relaunched, cfg := testDesktop(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "Taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Taken", "a.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, location, vname, inbox string
		is                           error
	}{
		{"not empty", root, "Taken", "Inbox", vault.ErrNotEmpty},
		{"separator in the name", root, "a/b", "Inbox", vault.ErrName},
		{"dot name", root, ".hidden", "Inbox", vault.ErrName},
		{"reserved", root, "CON", "Inbox", vault.ErrName},
		{"relative location", "docs", "Notes", "Inbox", vault.ErrLocation},
		{"root location", "/", "Notes", "Inbox", vault.ErrLocation},
		{"bad inbox", root, "Fine", "a/b", vault.ErrName},
	} {
		_, err := d.CreateVault(tt.location, tt.vname, tt.inbox, true)
		if !errors.Is(err, tt.is) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.is)
		}
	}
	if *relaunched != 0 {
		t.Error("relaunched after a refusal")
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Error("config written after refusals")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "Taken", "a.md")); string(b) != "mine" {
		t.Error("the existing folder was touched")
	}
	if _, err := os.Stat(filepath.Join(root, "Fine")); err == nil {
		t.Error("a vault folder was made although the inbox name was refused")
	}
}

func TestUseVaultChangesNothingButMeta(t *testing.T) {
	d, relaunched, cfg := testDesktop(t)
	dir := filepath.Join(t.TempDir(), "Writing")
	files := map[string]string{"a.md": "# A\n", "sub/b.md": "# B\n", "pic.png": "png"}
	for p, c := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := d.UseVault(dir, "Inbox", true) // welcome is on, but the folder is not empty
	if err != nil || res.Path != dir || *relaunched != 1 {
		t.Fatalf("%+v, %v, relaunched %d", res, err, *relaunched)
	}
	got := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			b, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(dir, p)
			got[filepath.ToSlash(rel)] = string(b)
		}
		return nil
	})
	if len(got) != len(files) {
		t.Fatalf("files after = %v, want exactly %v", got, files)
	}
	for p, c := range files {
		if got[p] != c {
			t.Errorf("%s = %q, want %q", p, got[p], c)
		}
	}
	if c, _ := vault.LoadConfig(cfg); c.Vault != dir || !c.Onboarded {
		t.Errorf("config %+v", c)
	}
}

func TestUseVaultEmptyFolderGetsTheWelcomeNoteOnRequest(t *testing.T) {
	for _, welcome := range []bool{true, false} {
		d, _, _ := testDesktop(t)
		dir := filepath.Join(t.TempDir(), "Empty")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := d.UseVault(dir, "Inbox", welcome); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(filepath.Join(dir, "Welcome.md"))
		if (err == nil) != welcome {
			t.Errorf("welcome=%v: Welcome.md exists = %v", welcome, err == nil)
		}
	}
}

func TestUseVaultRefusals(t *testing.T) {
	d, relaunched, _ := testDesktop(t)
	root := t.TempDir()
	file := filepath.Join(root, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(root, "missing"), file, "relative"} {
		if _, err := d.UseVault(p, "Inbox", false); err == nil {
			t.Errorf("UseVault(%q) accepted it", p)
		}
	}
	if _, err := d.UseVault(root, "../x", false); !errors.Is(err, vault.ErrName) {
		t.Errorf("bad inbox: %v", err)
	}
	if *relaunched != 0 {
		t.Error("relaunched after a refusal")
	}
}

func TestDefaultLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	d := &Desktop{}
	if got := d.DefaultLocation(); got != home {
		t.Errorf("without ~/Documents: %q, want %q", got, home)
	}
	if err := os.Mkdir(filepath.Join(home, "Documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := d.DefaultLocation(); got != filepath.Join(home, "Documents") {
		t.Errorf("with ~/Documents: %q", got)
	}
}

func TestVaultBeforeSetupAnswersWithAnError(t *testing.T) {
	s := &Vault{}
	if _, err := s.Tree(); !errors.Is(err, ErrNoVault) {
		t.Errorf("Tree: %v", err)
	}
	if _, err := s.Get("a.md"); !errors.Is(err, ErrNoVault) {
		t.Errorf("Get: %v", err)
	}
	if _, err := s.Create(CreateRequest{Title: "x"}); !errors.Is(err, ErrNoVault) {
		t.Errorf("Create: %v", err)
	}
	if got := s.Info(); got != (VaultInfo{}) {
		t.Errorf("Info = %+v", got)
	}
	if st := s.SyncStatus(); st.State != "off" {
		t.Errorf("SyncStatus = %+v", st)
	}
}

func TestWelcomeNoteShortcutsFollowThePlatform(t *testing.T) {
	if n := welcomeNote("darwin", "Inbox"); !strings.Contains(n, "⌘K") || !strings.Contains(n, "⌥⌘N") {
		t.Errorf("darwin:\n%s", n)
	}
	if n := welcomeNote("linux", "Inbox"); !strings.Contains(n, "Ctrl+K") || !strings.Contains(n, "Ctrl+Alt+N") {
		t.Errorf("linux:\n%s", n)
	}
}

// Signing in during setup, before the vault exists, links the vault the
// setup makes; abandoning the sign-in leaves no token behind.
func TestSignInDuringSetupLinksTheNewVault(t *testing.T) {
	f := &fakeServer{code: "one-time-code"}
	srv := httptest.NewServer(f)
	defer srv.Close()
	a, _, tokens, _ := testAccount(t, srv)
	a.metaDir, a.vaultName, a.restart = "", "", nil // no vault yet
	a.state = a.initial()
	if st := a.State(); st.State != "signed-out" {
		t.Fatalf("before setup: %+v", st)
	}
	signIn := func() {
		u, err := a.SignIn("")
		if err != nil {
			t.Fatal(err)
		}
		cu, _ := url.Parse(u)
		f.mu.Lock()
		f.challenge, f.used = cu.Query().Get("challenge"), false
		f.mu.Unlock()
		if err := a.CompleteSignIn("nota://auth?code=one-time-code&state=" + url.QueryEscape(cu.Query().Get("state"))); err != nil {
			t.Fatal(err)
		}
		if st := a.State(); st.State != "choose-vault" || st.Email != "anton@example.com" {
			t.Fatalf("after sign-in: %+v", st)
		}
	}

	// Abandon: the token is revoked on the server and forgotten here.
	signIn()
	if len(tokens) != 1 {
		t.Fatalf("tokens after sign-in = %v", tokens)
	}
	if err := a.SignOut(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !f.revoked || len(tokens) != 0 || a.State().State != "signed-out" {
		t.Fatalf("revoked %v, tokens %v, state %+v", f.revoked, tokens, a.State())
	}

	// Finish: CreateVault links the vault it made.
	signIn()
	d, relaunched, _ := testDesktop(t)
	d.acc = a
	res, err := d.CreateVault(t.TempDir(), "Notes", "Inbox", true)
	if err != nil {
		t.Fatal(err)
	}
	r, ok, err := nsync.LoadRemote(filepath.Join(res.Path, vault.MetaDirName))
	if err != nil || !ok || r.URL != srv.URL || r.Vault != "Notes" || r.TokenStore != storeApp {
		t.Fatalf("sync.toml = %+v %v %v", r, ok, err)
	}
	if len(f.created) != 1 || f.created[0] != "Notes" || *relaunched != 1 {
		t.Fatalf("created %v, relaunched %d", f.created, *relaunched)
	}
	if _, err := os.Stat(filepath.Join(res.Path, "Welcome.md")); err != nil {
		t.Errorf("welcome note missing after linking sync: %v", err)
	}
	if st := a.State(); st.State != "signed-in" || st.Vault != "Notes" {
		t.Errorf("after setup: %+v", st)
	}
}

func TestSetupSyncFailureStopsBeforeTheConfig(t *testing.T) {
	f := &fakeServer{code: "one-time-code"}
	srv := httptest.NewServer(f)
	a, _, _, _ := testAccount(t, srv)
	a.metaDir, a.vaultName = "", ""
	u, _ := a.SignIn("")
	cu, _ := url.Parse(u)
	f.challenge = cu.Query().Get("challenge")
	if err := a.CompleteSignIn("nota://auth?code=one-time-code&state=" + url.QueryEscape(cu.Query().Get("state"))); err != nil {
		t.Fatal(err)
	}
	srv.Close() // offline when finishing
	d, relaunched, cfg := testDesktop(t)
	d.acc = a
	_, err := d.CreateVault(t.TempDir(), "Notes", "Inbox", true)
	if err == nil || !strings.Contains(err.Error(), "set up sync") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(cfg); err == nil || *relaunched != 0 {
		t.Error("config written or relaunched although linking sync failed")
	}
}

func TestCheckLocationAnswersWithTheMessage(t *testing.T) {
	d := &Desktop{}
	if got := d.CheckLocation(t.TempDir()); got != "" {
		t.Errorf("a temp dir: %q", got)
	}
	if got := d.CheckLocation("/"); !strings.Contains(got, "Pick a folder inside your home folder") {
		t.Errorf("root: %q", got)
	}
	if got := d.CheckLocation("docs"); got != "Use a full path, starting with ~ or /." {
		t.Errorf("relative: %q", got)
	}
}
