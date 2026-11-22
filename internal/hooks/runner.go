package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/banjo/nota/internal/edit"
	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/markdown"
	"github.com/banjo/nota/internal/vault"
)

// OriginPrefix starts the Event.Origin of changes made by a hook.
const OriginPrefix = "hook:"

// maxParallel caps the after hooks running at the same time.
const maxParallel = 4

// Payload is the JSON a hook reads on stdin.
type Payload struct {
	Event events.Type `json:"event"`
	// Vault is the vault directory (also the hook's working directory).
	Vault string `json:"vault"`
	// Path is the note's vault-relative path (its new path for
	// note.moved), empty for sync.completed.
	Path    string `json:"path,omitempty"`
	OldPath string `json:"oldPath,omitempty"`
	Trash   string `json:"trash,omitempty"`
	Folder  string `json:"folder"`
	Title   string `json:"title,omitempty"`
	// Tags are the note's frontmatter and inline tags.
	Tags        []string       `json:"tags"`
	Frontmatter map[string]any `json:"frontmatter"`
	// Body is set for note.created, note.saved and inbox.added.
	Body   string    `json:"body,omitempty"`
	Time   time.Time `json:"time"`
	Origin string    `json:"origin,omitempty"`
}

// Patch is what a transform hook prints on stdout. Every field is
// optional; empty output means "no change".
type Patch struct {
	Set   map[string]any `json:"set,omitempty"`
	Unset []string       `json:"unset,omitempty"`
	Tags  struct {
		Add    []string `json:"add,omitempty"`
		Remove []string `json:"remove,omitempty"`
	} `json:"tags,omitzero"`
	// Folder moves the note; "/" is the vault root.
	Folder string `json:"folder,omitempty"`
}

// Empty reports whether the patch changes nothing.
func (p Patch) Empty() bool {
	return len(p.Set) == 0 && len(p.Unset) == 0 && len(p.Tags.Add) == 0 && len(p.Tags.Remove) == 0 && p.Folder == ""
}

// ParsePatch decodes a transform hook's output. Blank output is the empty
// patch; unknown fields are errors, so typos do not pass silently.
func ParsePatch(out []byte) (Patch, error) {
	var p Patch
	if len(bytes.TrimSpace(out)) == 0 {
		return p, nil
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Patch{}, fmt.Errorf("bad patch JSON: %w", err)
	}
	if dec.More() {
		return Patch{}, errors.New("bad patch JSON: more than one value")
	}
	return p, nil
}

// Exec runs a hook's command with stdin and returns its stdout. The
// context carries the hook's timeout.
type Exec func(ctx context.Context, h Hook, dir string, env []string, stdin []byte) (stdout []byte, err error)

// Options configure a Runner.
type Options struct {
	// Dir is the hooks' working directory; the vault root when empty.
	Dir string
	// Logger gets failures (warn) and runs (debug); slog.Default when nil.
	Logger *slog.Logger
	// Exec runs commands; ShellExec when nil. Tests replace it.
	Exec Exec
}

// Runner runs a vault's hooks on its events.
type Runner struct {
	v     *vault.Vault
	hooks []Hook
	dir   string
	log   *slog.Logger
	exec  Exec

	wg  sync.WaitGroup
	sem chan struct{}
}

// New returns a runner for the hooks in cfg. Attach it to the vault's bus.
func New(v *vault.Vault, cfg Config, opts Options) *Runner {
	r := &Runner{v: v, hooks: cfg.Hooks, dir: opts.Dir, log: opts.Logger, exec: opts.Exec, sem: make(chan struct{}, maxParallel)}
	if r.dir == "" {
		r.dir = v.Root()
	}
	if r.log == nil {
		r.log = slog.Default()
	}
	if r.exec == nil {
		r.exec = ShellExec
	}
	return r
}

// Hooks returns the configured hooks.
func (r *Runner) Hooks() []Hook { return r.hooks }

// Attach subscribes the runner to bus. Transform hooks run on the
// emitting goroutine, inside the write that triggered them; after hooks
// are started in the background. The returned func detaches it.
func (r *Runner) Attach(bus *events.Bus) (detach func()) {
	if bus == nil || len(r.hooks) == 0 {
		return func() {}
	}
	return bus.Subscribe(r.Handle)
}

// Wait blocks until every after hook started so far has finished.
// Commands call it before they exit so background hooks are not cut off.
func (r *Runner) Wait() { r.wg.Wait() }

// Handle runs the hooks for one event. A change a hook made never runs
// that hook again, and transform hooks never run on changes made by a
// transform hook, so hooks cannot loop.
func (r *Runner) Handle(e events.Event) {
	path := e.Path
	for _, h := range r.hooks {
		if h.Event != e.Type || r.skip(h, e) {
			continue
		}
		p, ok := r.payload(e, path)
		if !ok || !h.matches(p) {
			continue
		}
		if h.Mode == Transform {
			path = r.transform(h, p)
			continue
		}
		r.after(h, p)
	}
}

func (r *Runner) skip(h Hook, e events.Event) bool {
	// Notes that arrive through sync were already transformed on the
	// device that made them; transforming them again on every device
	// would fight over the same edit.
	if e.Origin == events.OriginSync && h.Mode == Transform {
		return true
	}
	by, ok := strings.CutPrefix(e.Origin, OriginPrefix)
	if !ok {
		return false
	}
	return by == h.ID || h.Mode == Transform
}

// payload builds the stdin JSON for an event on the note at path. It
// reports false when the note is gone (a transform hook moved it).
func (r *Runner) payload(e events.Event, path string) (Payload, bool) {
	p := Payload{Event: e.Type, Vault: r.v.Root(), Path: path, OldPath: e.OldPath, Trash: e.Trash,
		Tags: []string{}, Frontmatter: map[string]any{}, Time: e.Time, Origin: e.Origin}
	if path == "" {
		return p, true // sync.completed
	}
	src := path
	if e.Type == events.NoteDeleted {
		src = e.Trash
	}
	n, err := r.v.Read(src)
	if err != nil {
		if e.Type == events.NoteDeleted {
			p.Folder = vault.Folder(path)
			return p, true
		}
		return p, false
	}
	p.Folder = vault.Folder(path)
	p.Title = n.Title
	p.Frontmatter = n.Props
	p.Tags = edit.TagList(n.Props["tags"])
	for _, t := range markdown.Parse([]byte(n.Body), p.Folder).Tags {
		p.Tags = edit.AddTag(p.Tags, t)
	}
	switch e.Type {
	case events.NoteCreated, events.NoteSaved, events.InboxAdded:
		p.Body = n.Body
	}
	return p, true
}

func (h Hook) matches(p Payload) bool {
	if f := h.Match.Folder; f != "" {
		if p.Path == "" {
			return false
		}
		if !strings.EqualFold(p.Folder, f) && !hasPrefixFold(p.Folder, f+"/") {
			return false
		}
	}
	if t := h.Match.Tag; t != "" {
		return slices.ContainsFunc(p.Tags, func(x string) bool {
			return strings.EqualFold(x, t) || hasPrefixFold(x, t+"/")
		})
	}
	return true
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// Result is the outcome of one hook run.
type Result struct {
	Hook     string        `json:"hook"`
	Mode     Mode          `json:"mode"`
	Duration time.Duration `json:"-"`
	Seconds  float64       `json:"seconds"`
	Output   string        `json:"output,omitempty"`
	Patch    *Patch        `json:"patch,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// run executes a hook with its timeout.
func (r *Runner) run(h Hook, p Payload) ([]byte, time.Duration, error) {
	stdin, err := json.Marshal(p)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), h.Timeout)
	defer cancel()
	env := append(os.Environ(),
		"NOTA_VAULT="+r.v.Root(), "NOTA_EVENT="+string(p.Event), "NOTA_NOTE="+p.Path, "NOTA_HOOK="+h.ID)
	start := time.Now()
	out, err := r.exec(ctx, h, r.dir, env, stdin)
	d := time.Since(start)
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %v", h.Timeout)
	}
	return out, d, err
}

func (r *Runner) after(h Hook, p Payload) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.sem <- struct{}{}
		defer func() { <-r.sem }()
		_, d, err := r.run(h, p)
		if err != nil {
			r.log.Warn("hook failed", "hook", h.ID, "event", p.Event, "path", p.Path, "run", h.Run, "err", err)
			return
		}
		r.log.Debug("hook ran", "hook", h.ID, "event", p.Event, "path", p.Path, "duration", d.Round(time.Millisecond))
	}()
}

// transform runs a transform hook and applies its patch. It returns the
// note's path afterwards (moved or not).
func (r *Runner) transform(h Hook, p Payload) string {
	out, d, err := r.run(h, p)
	var patch Patch
	if err == nil {
		patch, err = ParsePatch(out)
	}
	if err == nil {
		var to string
		to, err = r.apply(h, p.Path, patch)
		if err == nil {
			r.log.Debug("transform hook ran", "hook", h.ID, "event", p.Event, "path", p.Path, "to", to, "duration", d.Round(time.Millisecond))
			return to
		}
	}
	r.log.Warn("transform hook failed; note left unchanged", "hook", h.ID, "event", p.Event, "path", p.Path, "run", h.Run, "err", err)
	return p.Path
}

// apply makes the patch's changes through the vault, so they are normal
// edits with events, marked as coming from the hook. The new content is
// checked before anything is written; the move happens first so a
// refused move leaves the note untouched.
func (r *Runner) apply(h Hook, path string, patch Patch) (string, error) {
	if patch.Empty() {
		return path, nil
	}
	hv := r.v.WithOrigin(OriginPrefix + h.ID)
	n, err := hv.Read(path)
	if err != nil {
		return path, err
	}
	src := n.Raw
	if len(patch.Set) > 0 || len(patch.Unset) > 0 {
		if src, err = edit.Props(src, patch.Set, patch.Unset); err != nil {
			return path, err
		}
	}
	if len(patch.Tags.Add) > 0 || len(patch.Tags.Remove) > 0 {
		if src, _, err = edit.Tags(src, patch.Tags.Add, patch.Tags.Remove); err != nil {
			return path, err
		}
	}
	if patch.Folder != "" {
		folder, err := vault.CleanPath(patch.Folder)
		if err != nil {
			return path, err
		}
		if folder != n.Folder() {
			if path, err = hv.Move(path, folder); err != nil {
				return n.Path, err
			}
		}
	}
	if !bytes.Equal(src, n.Raw) {
		if _, _, err := hv.Save(path, src); err != nil {
			return path, err
		}
	}
	return path, nil
}

// Test runs every hook for event on the note at path, as if the event had
// happened, and reports what each did. Patches are parsed but not
// applied; it is for `nota hooks test`.
func (r *Runner) Test(event events.Type, path string) ([]Result, error) {
	e := events.Event{Type: event, Path: path, Time: r.v.Now()}
	p, ok := r.payload(e, path)
	if !ok {
		return nil, fmt.Errorf("%s: %w", path, vault.ErrNotFound)
	}
	out := []Result{}
	for _, h := range r.hooks {
		if h.Event != event || !h.matches(p) {
			continue
		}
		o, d, err := r.run(h, p)
		res := Result{Hook: h.ID, Mode: h.Mode, Duration: d, Seconds: d.Seconds(), Output: string(o)}
		if err == nil && h.Mode == Transform {
			var patch Patch
			if patch, err = ParsePatch(o); err == nil {
				res.Patch = &patch
			}
		}
		if err != nil {
			res.Error = err.Error()
		}
		out = append(out, res)
	}
	return out, nil
}
