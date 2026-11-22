// Package hooks runs the scripts configured in <vault>/.nota/hooks.toml
// when core events happen, like git hooks:
//
//	[[hook]]
//	event = "inbox.added"        # see events.Types
//	run = "sh scripts/file.sh"   # run with the vault as working directory (sh -c; cmd /C on Windows)
//	mode = "transform"           # "after" (default) or "transform"
//	timeout = "2s"               # default 2s for transform, 30s for after
//	match = { folder = "Inbox", tag = "x" }
//
// A hook gets the event as JSON on stdin (see Payload) and NOTA_VAULT,
// NOTA_EVENT, NOTA_NOTE and NOTA_HOOK in its environment.
//
// After hooks run in the background and never block the write that
// triggered them. Transform hooks run only on note.created and
// inbox.added, before the write returns; they print a Patch as JSON on
// stdout, which is applied through the vault like any other edit. A
// failing, slow or confused hook is logged and leaves the note as it was.
package hooks

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/vault"
)

// File is the vault-relative path of the hooks configuration.
const File = vault.MetaDirName + "/hooks.toml"

// Mode says how a hook runs.
type Mode string

// Hook modes.
const (
	After     Mode = "after"
	Transform Mode = "transform"
)

// Default timeouts.
const (
	DefaultTransformTimeout = 2 * time.Second
	DefaultAfterTimeout     = 30 * time.Second
)

// TransformEvents are the events transform hooks may run on.
var TransformEvents = []events.Type{events.NoteCreated, events.InboxAdded}

// Match filters the notes a hook runs for. Empty fields match everything.
type Match struct {
	// Folder matches notes in this folder and its subfolders.
	Folder string `toml:"folder" json:"folder,omitempty"`
	// Tag matches notes with this tag in frontmatter or body, nested tags included.
	Tag string `toml:"tag" json:"tag,omitempty"`
}

// Hook is one [[hook]] entry.
type Hook struct {
	// ID names the hook in logs and event origins: its name, else "#N"
	// for the N-th entry.
	ID      string        `toml:"-" json:"id"`
	Name    string        `toml:"name" json:"name,omitempty"`
	Event   events.Type   `toml:"event" json:"event"`
	Run     string        `toml:"run" json:"run"`
	Mode    Mode          `toml:"mode" json:"mode"`
	Timeout time.Duration `toml:"-" json:"-"`
	Match   Match         `toml:"match" json:"match"`

	RawTimeout string `toml:"timeout" json:"-"`
}

// Config is the parsed hooks file.
type Config struct {
	Hooks []Hook `toml:"hook"`
}

// Load reads the vault's hooks file. A missing file means no hooks.
func Load(v *vault.Vault) (Config, error) {
	data, err := v.FS().ReadFile(File)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("hooks: %w", err)
	}
	c, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("hooks: %s: %w", File, err)
	}
	return c, nil
}

// Parse decodes and validates a hooks file, filling in defaults.
func Parse(data []byte) (Config, error) {
	var c Config
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return Config{}, err
	}
	if und := md.Undecoded(); len(und) > 0 {
		keys := make([]string, len(und))
		for i, k := range und {
			keys[i] = k.String()
		}
		return Config{}, fmt.Errorf("unknown keys: %s", strings.Join(keys, ", "))
	}
	seen := map[string]bool{}
	for i := range c.Hooks {
		h := &c.Hooks[i]
		h.ID = h.Name
		if h.ID == "" {
			h.ID = "#" + strconv.Itoa(i+1)
		}
		if seen[h.ID] {
			return Config{}, fmt.Errorf("hook %s: duplicate name", h.ID)
		}
		seen[h.ID] = true
		if err := h.validate(); err != nil {
			return Config{}, fmt.Errorf("hook %s: %w", h.ID, err)
		}
	}
	return c, nil
}

func (h *Hook) validate() error {
	if !slices.Contains(events.Types, h.Event) {
		return fmt.Errorf("unknown event %q; want one of %s", h.Event, typeList(events.Types))
	}
	if strings.TrimSpace(h.Run) == "" {
		return errors.New(`"run" is empty`)
	}
	switch h.Mode {
	case "":
		h.Mode = After
	case After:
	case Transform:
		if !slices.Contains(TransformEvents, h.Event) {
			return fmt.Errorf("transform hooks run only on %s, not %s", typeList(TransformEvents), h.Event)
		}
	default:
		return fmt.Errorf(`unknown mode %q; want "after" or "transform"`, h.Mode)
	}
	switch {
	case h.RawTimeout != "":
		d, err := time.ParseDuration(h.RawTimeout)
		if err != nil || d <= 0 {
			return fmt.Errorf("timeout %q: want a positive duration such as \"2s\"", h.RawTimeout)
		}
		h.Timeout = d
	case h.Mode == Transform:
		h.Timeout = DefaultTransformTimeout
	default:
		h.Timeout = DefaultAfterTimeout
	}
	if h.Match.Folder != "" {
		f, err := vault.CleanPath(h.Match.Folder)
		if err != nil {
			return fmt.Errorf("match.folder: %w", err)
		}
		h.Match.Folder = f // "" (the root) matches every note
	}
	return nil
}

func typeList(ts []events.Type) string {
	s := make([]string, len(ts))
	for i, t := range ts {
		s[i] = string(t)
	}
	return strings.Join(s, ", ")
}
