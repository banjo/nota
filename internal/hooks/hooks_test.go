package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/vault"
)

func TestParse(t *testing.T) {
	c, err := Parse([]byte(`
[[hook]]
event = "note.saved"
run = "./x.sh"

[[hook]]
name = "file"
event = "inbox.added"
run = "sort"
mode = "transform"
timeout = "500ms"
match = { folder = "/Inbox/", tag = "#x" }
`))
	if err != nil {
		t.Fatal(err)
	}
	h0, h1 := c.Hooks[0], c.Hooks[1]
	if h0.ID != "#1" || h0.Mode != After || h0.Timeout != DefaultAfterTimeout {
		t.Fatalf("defaults: %+v", h0)
	}
	if h1.ID != "file" || h1.Mode != Transform || h1.Timeout != 500*time.Millisecond || h1.Match.Folder != "Inbox" {
		t.Fatalf("hook 2: %+v", h1)
	}
	for _, bad := range []struct{ toml, want string }{
		{`[[hook]]` + "\n" + `event = "note.typed"` + "\n" + `run = "x"`, "unknown event"},
		{`[[hook]]` + "\n" + `event = "note.saved"` + "\n" + `run = "x"` + "\n" + `mode = "transform"`, "transform hooks run only on"},
		{`[[hook]]` + "\n" + `event = "note.saved"`, `"run" is empty`},
		{`[[hook]]` + "\n" + `event = "note.saved"` + "\n" + `run = "x"` + "\n" + `timeout = "soon"`, "timeout"},
		{`[[hook]]` + "\n" + `event = "note.saved"` + "\n" + `run = "x"` + "\n" + `cmd = "y"`, "unknown keys: hook.cmd"},
		{`[[hook]]` + "\n" + `event = "note.saved"` + "\n" + `run = "x"` + "\n" + `mode = "before"`, "unknown mode"},
		{`[[hook]`, "toml"},
	} {
		if _, err := Parse([]byte(bad.toml)); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("Parse(%q) = %v, want %q", bad.toml, err, bad.want)
		}
	}
}

func TestParsePatch(t *testing.T) {
	p, err := ParsePatch([]byte(`{"set":{"a":1},"unset":["b"],"tags":{"add":["x"],"remove":["y"]},"folder":"F"}`))
	if err != nil || p.Folder != "F" || p.Tags.Add[0] != "x" || p.Empty() {
		t.Fatalf("%+v %v", p, err)
	}
	if p, err := ParsePatch([]byte("  \n")); err != nil || !p.Empty() {
		t.Fatalf("blank: %+v %v", p, err)
	}
	for _, bad := range []string{"not json", `{"folders":"x"}`, `{} {}`, `[1]`} {
		if _, err := ParsePatch([]byte(bad)); err == nil {
			t.Errorf("ParsePatch(%q) should fail", bad)
		}
	}
}

// fake is an Exec that records calls and answers from a table.
type fake struct {
	mu    sync.Mutex
	calls []Payload
	env   [][]string
	reply func(h Hook, p Payload) (string, error)
}

func (f *fake) exec(ctx context.Context, h Hook, _ string, env []string, stdin []byte) ([]byte, error) {
	var p Payload
	if err := json.Unmarshal(stdin, &p); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, p)
	f.env = append(f.env, env)
	f.mu.Unlock()
	if f.reply == nil {
		return nil, nil
	}
	out, err := f.reply(h, p)
	return []byte(out), err
}

func (f *fake) hooksCalled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, p := range f.calls {
		out = append(out, string(p.Event)+" "+p.Path)
	}
	return out
}

type setup struct {
	v    *vault.Vault
	m    *vault.MemFS
	bus  *events.Bus
	r    *Runner
	log  *bytes.Buffer
	seen []string
}

func newSetup(t *testing.T, hooksTOML string, f *fake, files map[string]string) *setup {
	t.Helper()
	cfg, err := Parse([]byte(hooksTOML))
	if err != nil {
		t.Fatal(err)
	}
	s := &setup{m: vault.NewMemFS(files), bus: events.New(), log: &bytes.Buffer{}}
	s.v, err = vault.OpenFS(s.m, "mem", vault.Options{Bus: s.bus})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	s.bus.Subscribe(func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		x := string(e.Type) + " " + e.Path
		if e.Origin != "" {
			x += " [" + e.Origin + "]"
		}
		s.seen = append(s.seen, x)
	})
	s.r = New(s.v, cfg, Options{Exec: f.exec, Logger: slog.New(slog.NewTextHandler(&lockedWriter{w: s.log}, nil))})
	s.r.Attach(s.bus)
	return s
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (s *setup) read(t *testing.T, name string) string {
	t.Helper()
	b, err := s.m.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTransformFilesInboxNote(t *testing.T) {
	f := &fake{reply: func(h Hook, p Payload) (string, error) {
		if !strings.Contains(p.Body, "#jobb") {
			return "", nil
		}
		return `{"folder":"Jobb","tags":{"add":["sorterad"]},"set":{"status":"ny"}}`, nil
	}}
	s := newSetup(t, `
[[hook]]
name = "sort"
event = "inbox.added"
mode = "transform"
run = "x"
`, f, nil)
	n, err := s.v.Create("Inbox", []byte("---\ncreated: 2026-01-01\n---\nRing Anna #jobb\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n.Path != "Inbox/Ring Anna jobb.md" {
		t.Fatalf("created %q", n.Path)
	}
	got := s.read(t, "Jobb/Ring Anna jobb.md")
	if !strings.HasPrefix(got, "---\ncreated: 2026-01-01\nstatus: ny\ntags: [sorterad]\nupdated: ") || !strings.HasSuffix(got, "---\nRing Anna #jobb\n") {
		t.Fatalf("filed note:\n%s", got)
	}
	want := []string{
		"note.created Inbox/Ring Anna jobb.md",
		"inbox.added Inbox/Ring Anna jobb.md",
		"note.moved Jobb/Ring Anna jobb.md [hook:sort]",
		"note.saved Jobb/Ring Anna jobb.md [hook:sort]",
	}
	if !slices.Equal(s.seen, want) {
		t.Fatalf("events:\n%s", strings.Join(s.seen, "\n"))
	}
	// Notes without #jobb stay.
	if _, err := s.v.Create("Inbox", []byte("Handla\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.m.Stat("Inbox/Handla.md"); err != nil {
		t.Fatal(err)
	}
	payload := f.calls[0]
	if payload.Event != events.InboxAdded || payload.Vault != "mem" || payload.Title != "Ring Anna #jobb" ||
		payload.Frontmatter["created"] == nil || !slices.Equal(payload.Tags, []string{"jobb"}) {
		t.Fatalf("payload %+v", payload)
	}
	if env := strings.Join(f.env[0], "\n"); !strings.Contains(env, "NOTA_EVENT=inbox.added") || !strings.Contains(env, "NOTA_VAULT=mem") ||
		!strings.Contains(env, "NOTA_NOTE=Inbox/Ring Anna jobb.md") || !strings.Contains(env, "NOTA_HOOK=sort") {
		t.Fatalf("env %q", f.env[0])
	}
}

func TestTransformFailuresLeaveNote(t *testing.T) {
	replies := map[string]string{
		"bad-json": "not json",
		"typo":     `{"folders":"X"}`,
		"escape":   `{"folder":"../out"}`,
	}
	f := &fake{reply: func(h Hook, p Payload) (string, error) {
		if h.ID == "fail" {
			return "", errors.New("exit status 1")
		}
		return replies[h.ID], nil
	}}
	toml := ""
	for _, id := range []string{"bad-json", "typo", "escape", "fail"} {
		toml += fmt.Sprintf("[[hook]]\nname = %q\nevent = \"note.created\"\nmode = \"transform\"\nrun = \"x\"\n", id)
	}
	s := newSetup(t, toml, f, map[string]string{"Taken/x.md": "x"})
	content := "---\na: 1\n---\nx\n"
	if _, err := s.v.Create("", []byte(content)); err != nil {
		t.Fatal(err)
	}
	if got := s.read(t, "x.md"); got != content {
		t.Fatalf("note changed: %q", got)
	}
	logs := s.log.String()
	for _, want := range []string{"hook=bad-json", "hook=typo", "hook=escape", "hook=fail", "note left unchanged"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log lacks %q:\n%s", want, logs)
		}
	}
	if len(f.calls) != 4 {
		t.Fatalf("calls %v", f.hooksCalled())
	}
}

func TestTransformMoveRefusedLeavesNote(t *testing.T) {
	f := &fake{reply: func(Hook, Payload) (string, error) {
		return `{"folder":"Taken","set":{"b":2}}`, nil
	}}
	s := newSetup(t, "[[hook]]\nevent = \"note.created\"\nmode = \"transform\"\nrun = \"x\"\n", f,
		map[string]string{"Taken/x.md": "other"})
	if _, err := s.v.Create("", []byte("---\na: 1\n---\nx\n")); err != nil {
		t.Fatal(err)
	}
	if got := s.read(t, "x.md"); got != "---\na: 1\n---\nx\n" {
		t.Fatalf("note changed: %q", got)
	}
	if !strings.Contains(s.log.String(), "already exists") {
		t.Fatalf("log: %s", s.log.String())
	}
}

func TestNoLoops(t *testing.T) {
	// A transform on note.created that writes, and an after hook on
	// note.saved: the transform's save runs the after hook once and
	// never the transform again; a change marked as coming from a hook
	// does not run that hook.
	f := &fake{reply: func(h Hook, p Payload) (string, error) {
		if h.ID == "stamp" {
			return `{"set":{"stamped":true}}`, nil
		}
		return "", nil
	}}
	s := newSetup(t, `
[[hook]]
name = "stamp"
event = "note.created"
mode = "transform"
run = "x"

[[hook]]
name = "log"
event = "note.saved"
run = "x"
`, f, nil)
	if _, err := s.v.Create("Notes", []byte("---\na: 1\n---\nx\n")); err != nil {
		t.Fatal(err)
	}
	s.r.Wait()
	s.r.Handle(events.Event{Type: events.NoteSaved, Path: "Notes/x.md", Origin: OriginPrefix + "log"})
	s.r.Handle(events.Event{Type: events.NoteCreated, Path: "Notes/x.md", Origin: OriginPrefix + "other"})
	s.r.Wait()
	got := f.hooksCalled()
	want := []string{"note.created Notes/x.md", "note.saved Notes/x.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("calls %q, want %q", got, want)
	}
	if f.calls[1].Origin != "hook:stamp" || f.calls[1].Body != "x\n" {
		t.Fatalf("after payload %+v", f.calls[1])
	}
}

func TestMatch(t *testing.T) {
	f := &fake{}
	s := newSetup(t, `
[[hook]]
name = "folder"
event = "note.saved"
run = "x"
match = { folder = "projekt" }

[[hook]]
name = "tag"
event = "note.saved"
run = "x"
match = { tag = "a" }
`, f, map[string]string{
		"Projekt/Sub/one.md": "one",
		"Other/two.md":       "---\ntags: [a/b]\n---\ntwo",
		"Other/three.md":     "three #A",
		"Other/four.md":      "four #ab",
	})
	for _, p := range []string{"Projekt/Sub/one.md", "Other/two.md", "Other/three.md", "Other/four.md"} {
		if _, _, err := s.v.Write(p, []byte(s.read(t, p)+" edit")); err != nil {
			t.Fatal(err)
		}
	}
	s.r.Wait()
	got := f.hooksCalled()
	slices.Sort(got)
	want := []string{"note.saved Other/three.md", "note.saved Other/two.md", "note.saved Projekt/Sub/one.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("calls %q, want %q", got, want)
	}
}

func TestDeletedAndMovedPayloads(t *testing.T) {
	f := &fake{}
	s := newSetup(t, "[[hook]]\nevent = \"note.deleted\"\nrun = \"x\"\n[[hook]]\nevent = \"note.moved\"\nrun = \"x\"\n", f,
		map[string]string{"A/n.md": "---\ntags: [t]\n---\nN\n"})
	if _, err := s.v.Move("A/n.md", "B"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.v.Delete("B/n.md"); err != nil {
		t.Fatal(err)
	}
	s.r.Wait()
	slices.SortFunc(f.calls, func(a, b Payload) int { return strings.Compare(string(a.Event), string(b.Event)) })
	del, mv := f.calls[0], f.calls[1]
	if del.Path != "B/n.md" || del.Trash != ".trash/B/n.md" || del.Title != "N" || del.Body != "" {
		t.Fatalf("deleted payload %+v", del)
	}
	if mv.Path != "B/n.md" || mv.OldPath != "A/n.md" || !slices.Equal(mv.Tags, []string{"t"}) {
		t.Fatalf("moved payload %+v", mv)
	}
}

// TestAfterHooksConcurrent emits from many goroutines; run with -race.
func TestAfterHooksConcurrent(t *testing.T) {
	f := &fake{reply: func(Hook, Payload) (string, error) {
		time.Sleep(time.Millisecond)
		return "", nil
	}}
	s := newSetup(t, "[[hook]]\nevent = \"note.saved\"\nrun = \"x\"\n", f, map[string]string{"a.md": "a", "b.md": "b"})
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.r.Handle(events.Event{Type: events.NoteSaved, Path: []string{"a.md", "b.md"}[i%2]})
		}()
	}
	wg.Wait()
	s.r.Wait()
	if n := len(f.hooksCalled()); n != 20 {
		t.Fatalf("%d calls, want 20", n)
	}
}

func TestTestDryRun(t *testing.T) {
	f := &fake{reply: func(Hook, Payload) (string, error) { return `{"folder":"X"}`, nil }}
	s := newSetup(t, "[[hook]]\nevent = \"inbox.added\"\nmode = \"transform\"\nrun = \"x\"\n", f, map[string]string{"Inbox/a.md": "a"})
	res, err := s.r.Test(events.InboxAdded, "Inbox/a.md")
	if err != nil || len(res) != 1 || res[0].Patch == nil || res[0].Patch.Folder != "X" {
		t.Fatalf("Test = %+v %v", res, err)
	}
	if _, err := s.m.Stat("Inbox/a.md"); err != nil {
		t.Fatal("dry run moved the note")
	}
	if _, err := s.r.Test(events.InboxAdded, "Inbox/missing.md"); err == nil {
		t.Fatal("missing note should fail")
	}
}

func TestShellExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	ctx := context.Background()
	out, err := ShellExec(ctx, Hook{Run: `read -r line; echo "$line $FOO $(pwd)"`}, dir, []string{"FOO=bar"}, []byte("hej\n"))
	if err != nil || strings.TrimSpace(string(out)) != "hej bar "+dir {
		t.Fatalf("out %q err %v", out, err)
	}
	if _, err := ShellExec(ctx, Hook{Run: "echo oops >&2; exit 3"}, dir, nil, nil); err == nil || !strings.Contains(err.Error(), "exit status 3: oops") {
		t.Fatalf("exit: %v", err)
	}
	// A timeout kills the shell and its children, quickly.
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := ShellExec(ctx, Hook{Run: "sleep 10 & sleep 10"}, dir, nil, nil); err == nil {
		t.Fatal("timeout should fail")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
}
