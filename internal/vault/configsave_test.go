package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nota", "config.toml") // directory made on demand
	want := Config{Vault: `/home/me/My "Notes"`, Inbox: "Capture", Onboarded: true}
	if err := SaveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	// A Windows path survives TOML quoting.
	want.Vault = `C:\Users\me\Notes`
	if err := SaveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	if got, _ = LoadConfig(path); got != want {
		t.Fatalf("windows path: got %+v, want %+v", got, want)
	}
}

func TestSaveConfigKeepsTheRest(t *testing.T) {
	tests := []struct {
		name, in string
		set      Config
		want     string
	}{
		{
			name: "replaces in place, keeps comments, unknown keys and tables",
			in:   "# my notes\nvault = \"/old\"   \ntheme = \"dark\"\ninbox = \"Old\"\n\n[sync]\nserver = \"https://x\"\nvault = \"remote\"\n",
			set:  Config{Vault: "/new", Inbox: "Capture", Onboarded: true},
			want: "# my notes\nvault = \"/new\"\ntheme = \"dark\"\ninbox = \"Capture\"\nonboarded = true\n\n[sync]\nserver = \"https://x\"\nvault = \"remote\"\n",
		},
		{
			name: "adds missing keys before the first table",
			in:   "theme = \"dark\"\n\n[sync]\nserver = \"s\"\n",
			set:  Config{Vault: "/v"},
			want: "theme = \"dark\"\nvault = \"/v\"\n\n[sync]\nserver = \"s\"\n",
		},
		{
			name: "no trailing newline",
			in:   "theme = \"dark\"",
			set:  Config{Vault: "/v"},
			want: "theme = \"dark\"\nvault = \"/v\"\n",
		},
		{
			name: "empty fields leave the file alone",
			in:   "vault = \"/keep\"\ninbox = \"Keep\"\n",
			set:  Config{Onboarded: false},
			want: "vault = \"/keep\"\ninbox = \"Keep\"\n",
		},
		{
			name: "a multi-line string that looks like a table",
			in:   "note = \"\"\"\n[not a table]\n\"\"\"\n",
			set:  Config{Vault: "/v"},
			want: "note = \"\"\"\n[not a table]\n\"\"\"\nvault = \"/v\"\n",
		},
		{
			name: "CRLF files stay CRLF",
			in:   "theme = \"dark\"\r\n",
			set:  Config{Vault: "/v"},
			want: "theme = \"dark\"\r\nvault = \"/v\"\r\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(tt.in), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := SaveConfig(path, tt.set); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(path)
			if string(b) != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", b, tt.want)
			}
			if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
				t.Errorf("mode %v, want 0600 kept", fi.Mode().Perm())
			}
		})
	}
}

func TestSaveConfigRefusesBrokenFileAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	broken := "vault = \n[oops"
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveConfig(path, Config{Vault: "/v"}); err == nil {
		t.Fatal("want an error for a file that is not TOML")
	}
	if b, _ := os.ReadFile(path); string(b) != broken {
		t.Errorf("file changed: %q", b)
	}
	es, _ := os.ReadDir(dir)
	if len(es) != 1 {
		t.Errorf("leftover files: %v", es)
	}
}

func TestNeedsOnboarding(t *testing.T) {
	home := t.TempDir()
	notes := filepath.Join(home, "Notes")
	tests := []struct {
		name  string
		setup func(t *testing.T)
		loc   Location
		want  bool
	}{
		{"fresh: no ~/Notes", func(*testing.T) {}, Location{Dir: notes, Source: "default"}, true},
		{"fresh: empty ~/Notes", func(t *testing.T) {
			if err := os.MkdirAll(notes, 0o755); err != nil {
				t.Fatal(err)
			}
		}, Location{Dir: notes, Source: "default"}, true},
		{"non-empty ~/Notes: an existing user", func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(notes, "a.md"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, Location{Dir: notes, Source: "default"}, false},
		{"NOTA_VAULT", func(*testing.T) {}, Location{Dir: "/nowhere", Source: "env"}, false},
		{"flag", func(*testing.T) {}, Location{Dir: "/nowhere", Source: "flag"}, false},
		{"config vault key", func(*testing.T) {}, Location{Dir: "/nowhere", Source: "config", Config: Config{Vault: "/nowhere"}}, false},
		{"finished onboarding before", func(*testing.T) {}, Location{Dir: filepath.Join(home, "Gone"), Source: "default", Config: Config{Onboarded: true}}, false},
		{"config without a vault key, fresh", func(*testing.T) {}, Location{Dir: filepath.Join(home, "Fresh"), Source: "default", Config: Config{Inbox: "Capture"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			got, err := NeedsOnboarding(tt.loc)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("NeedsOnboarding = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("a file where ~/Notes should be is not a first run", func(t *testing.T) {
		f := filepath.Join(home, "file")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := NeedsOnboarding(Location{Dir: f, Source: "default"})
		if got || err == nil || !strings.Contains(err.Error(), "look at") {
			t.Errorf("got %v, %v; want false and an error", got, err)
		}
	})
}

// Discover and NeedsOnboarding together, with the env and the config
// file the way the app sees them.
func TestNeedsOnboardingFromDiscover(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv(EnvVault, "")
	check := func(want bool, why string) {
		t.Helper()
		loc, err := Discover("")
		if err != nil {
			t.Fatal(err)
		}
		got, err := NeedsOnboarding(loc)
		if err != nil || got != want {
			t.Fatalf("%s: got %v, %v; want %v", why, got, err, want)
		}
	}
	check(true, "clean home")
	cfg, _ := ConfigPath()
	if err := SaveConfig(cfg, Config{Vault: filepath.Join(home, "Mine"), Onboarded: true}); err != nil {
		t.Fatal(err)
	}
	check(false, "after onboarding wrote the config")
	t.Setenv(EnvVault, filepath.Join(home, "FromEnv"))
	check(false, "with NOTA_VAULT")
}
