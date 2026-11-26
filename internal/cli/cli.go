// Package cli implements the nota command line. Every command works on
// one vault (found by vault.Discover), keeps the index up to date before
// it reads from it, and prints JSON instead of text with --json.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/edit"
	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/hooks"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/vault"
)

// Version is set at build time with -ldflags "-X github.com/banjo/nota/internal/cli.Version=…".
var Version = "dev"

// Exit codes. Scripts can rely on them.
const (
	ExitOK        = 0
	ExitError     = 1 // anything else: IO, broken index, editor failed
	ExitUsage     = 2 // bad flags or arguments
	ExitNotFound  = 3 // no such note, view or vault
	ExitAmbiguous = 4 // a note reference matched several notes
	ExitConflict  = 5 // the target exists or differs only in case
)

// Error is an error with an exit code and a stable machine-readable kind.
type Error struct {
	Code    int      `json:"-"`
	Kind    string   `json:"code"`
	Message string   `json:"message"`
	Matches []string `json:"matches,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func usageErr(format string, args ...any) error {
	return &Error{Code: ExitUsage, Kind: "usage", Message: fmt.Sprintf(format, args...)}
}

// classify maps any error onto an *Error.
func classify(err error) *Error {
	var ce *Error
	if errors.As(err, &ce) {
		return ce
	}
	msg := err.Error()
	switch {
	case errors.Is(err, vault.ErrNotFound):
		return &Error{Code: ExitNotFound, Kind: "not_found", Message: msg}
	case errors.Is(err, vault.ErrCaseCollision):
		return &Error{Code: ExitConflict, Kind: "case_collision", Message: msg}
	case errors.Is(err, vault.ErrExists):
		return &Error{Code: ExitConflict, Kind: "exists", Message: msg}
	case errors.Is(err, edit.ErrInvalidTag):
		return &Error{Code: ExitUsage, Kind: "usage", Message: msg}
	case errors.Is(err, vault.ErrInvalidPath), errors.Is(err, index.ErrEmptyQuery):
		return &Error{Code: ExitUsage, Kind: "invalid", Message: msg}
	case strings.HasPrefix(msg, "unknown command"), strings.HasPrefix(msg, "unknown flag"),
		strings.HasPrefix(msg, "unknown shorthand flag"), strings.Contains(msg, "flag needs an argument"),
		strings.HasPrefix(msg, "invalid argument"):
		return &Error{Code: ExitUsage, Kind: "usage", Message: msg}
	}
	return &Error{Code: ExitError, Kind: "error", Message: msg}
}

// app is the state shared by the commands of one invocation.
type app struct {
	vaultFlag string
	json      bool
	verbose   bool

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	log    *slog.Logger

	loc   vault.Location
	vault *vault.Vault
	bus   *events.Bus
	ix    *index.Index
	hooks *hooks.Runner
	// attrs are added to the command's wide log event.
	attrs []any
}

// Main runs the CLI with args (without the program name) and returns the
// process exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a := &app{stdin: stdin, stdout: stdout, stderr: stderr}
	a.log = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	start := time.Now()
	cmd, err := root.ExecuteC()
	code := ExitOK
	if err != nil {
		ce := classify(err)
		code = ce.Code
		a.printError(ce)
	}
	a.close()
	name := "nota"
	if cmd != nil {
		name = cmd.CommandPath()
	}
	a.log.Debug("command",
		append([]any{"cmd", name, "vault", a.loc.Dir, "vault_source", a.loc.Source,
			"exit", code, "duration", time.Since(start).Round(time.Microsecond)}, a.attrs...)...)
	return code
}

func (a *app) printError(e *Error) {
	if a.json {
		enc := json.NewEncoder(a.stderr)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(map[string]*Error{"error": e})
		return
	}
	fmt.Fprintf(a.stderr, "nota: %s\n", e.Message)
	for _, m := range e.Matches {
		fmt.Fprintf(a.stderr, "  %s\n", m)
	}
	if e.Code == ExitUsage && e.Kind == "usage" {
		fmt.Fprintln(a.stderr, "Run 'nota --help' for usage.")
	}
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "nota",
		Short: "Local-first markdown notes",
		Long: `nota manages a vault of plain markdown notes.

The vault is --vault, else $NOTA_VAULT, else "vault" in
~/.config/nota/config.toml, else ~/Notes. Notes are addressed by path
("Projekt/Plan.md", the .md is optional), title or a unique prefix.
Every command prints JSON with --json.`,
		Version:       Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			level := slog.LevelWarn
			if a.verbose {
				level = slog.LevelDebug
			}
			a.log = slog.New(slog.NewTextHandler(a.stderr, &slog.HandlerOptions{Level: level}))
			slog.SetDefault(a.log)
		},
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageErr("%v", err) })
	pf := root.PersistentFlags()
	pf.StringVar(&a.vaultFlag, "vault", "", "vault directory (overrides $NOTA_VAULT and the config file)")
	pf.BoolVar(&a.json, "json", false, "print JSON")
	pf.BoolVarP(&a.verbose, "verbose", "v", false, "log debug information to stderr")
	_ = root.MarkPersistentFlagDirname("vault")

	root.AddCommand(
		a.addCmd(), a.lsCmd(), a.searchCmd(), a.catCmd(), a.openCmd(), a.mvCmd(), a.folderCmd(),
		a.rmCmd(), a.tagCmd(), a.setCmd(), a.pinCmd(), a.unpinCmd(), a.replaceCmd(), a.exportCmd(), a.inboxCmd(), a.indexCmd(), a.viewsCmd(), a.pluginsCmd(),
		a.backlinksCmd(), a.tagsCmd(), a.mcpCmd(), a.hooksCmd(), a.serveCmd(),
		a.syncCmd(), a.remoteCmd(), a.loginCmd(), a.logoutCmd(), a.devicesCmd(),
		a.historyCmd(), a.restoreCmd(), a.trashCmd(), a.settingsCmd(),
	)
	return root
}

// openVault finds and opens the vault. create makes a missing vault
// directory (only `nota add` does that).
func (a *app) openVault(create bool) error {
	if a.vault != nil {
		return nil
	}
	loc, err := vault.Discover(a.vaultFlag)
	if err != nil {
		return err
	}
	a.loc = loc
	if !create {
		if st, err := os.Stat(loc.Dir); err != nil || !st.IsDir() {
			return &Error{Code: ExitNotFound, Kind: "no_vault",
				Message: fmt.Sprintf("no vault at %s (from %s); set --vault or $NOTA_VAULT, or create it with `nota add`", loc.Dir, loc.Source)}
		}
	}
	now, err := clock()
	if err != nil {
		return err
	}
	a.bus = events.New()
	v, err := vault.Open(loc.Dir, vault.Options{Inbox: loc.Config.Inbox, Create: create, Bus: a.bus, Now: now})
	if err != nil {
		return err
	}
	a.vault = v
	// Keep the index in step with every write this command makes.
	a.bus.Subscribe(a.onEvent)
	// Then run the vault's hooks on them. A broken hooks file is reported
	// but does not stop the command.
	if cfg, err := hooks.Load(v); err != nil {
		a.log.Warn("hooks disabled", "err", err)
	} else if len(cfg.Hooks) > 0 {
		a.hooks = hooks.New(v, cfg, hooks.Options{Logger: a.log})
		a.hooks.Attach(a.bus)
	}
	a.log.Debug("vault", "dir", loc.Dir, "source", loc.Source, "inbox", v.Inbox())
	return nil
}

// EnvNow fixes the clock for timestamps written into notes (RFC 3339).
// It exists for tests and reproducible scripts.
const EnvNow = "NOTA_NOW"

// clock returns the time source: $NOTA_NOW when set, else the real clock.
func clock() (func() time.Time, error) {
	s := os.Getenv(EnvNow)
	if s == "" {
		return time.Now, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, usageErr("$%s: %v", EnvNow, err)
	}
	return func() time.Time { return t }, nil
}

// index opens the vault's index and brings it up to date with the files.
func (a *app) index() (*index.Index, error) {
	if a.ix != nil {
		return a.ix, nil
	}
	if err := a.openVault(false); err != nil {
		return nil, err
	}
	ix, err := index.OpenVault(a.vault)
	if err != nil {
		return nil, err
	}
	st, err := ix.Sync(a.vault)
	if err != nil {
		ix.Close()
		return nil, err
	}
	a.ix = ix
	a.attrs = append(a.attrs, "index_notes", st.Notes, "index_changed", st.Added+st.Updated+st.Removed,
		"index_sync", st.Duration.Round(time.Microsecond), "index_recreated", ix.Recreated)
	return ix, nil
}

func (a *app) onEvent(e events.Event) {
	a.log.Debug("event", "type", e.Type, "path", e.Path, "old_path", e.OldPath)
	if a.ix == nil {
		return // the next command's sync picks the change up
	}
	var err error
	switch e.Type {
	case events.NoteMoved:
		if err = a.ix.Remove(e.OldPath); err == nil {
			err = a.ix.Update(a.vault, e.Path)
		}
	case events.NoteDeleted:
		err = a.ix.Remove(e.Path)
	case events.NoteCreated, events.NoteSaved:
		err = a.ix.Update(a.vault, e.Path)
	}
	if err != nil {
		a.log.Warn("index update failed; run `nota index`", "path", e.Path, "err", err)
	}
}

func (a *app) close() {
	if a.hooks != nil {
		a.hooks.Wait() // let background hooks finish before the process exits
	}
	a.bus.Close()
	if a.ix != nil {
		a.ix.Close()
	}
}

// printJSONLine writes v as one line of JSON (for streams of results).
func (a *app) printJSONLine(v any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// printJSON writes v as indented JSON.
func (a *app) printJSON(v any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// resolve finds the note a user reference points to: an existing path,
// else an exact title or file name, else a unique prefix.
func (a *app) resolve(ref string) (string, error) {
	if err := a.openVault(false); err != nil {
		return "", err
	}
	if p, err := vault.NotePath(ref); err == nil && a.vault.Exists(p) {
		return p, nil
	}
	ix, err := a.index()
	if err != nil {
		return "", err
	}
	ms, err := ix.Lookup(ref)
	if err != nil {
		return "", err
	}
	switch len(ms) {
	case 0:
		return "", &Error{Code: ExitNotFound, Kind: "not_found", Message: fmt.Sprintf("no note matches %q", ref)}
	case 1:
		return ms[0], nil
	}
	return "", &Error{Code: ExitAmbiguous, Kind: "ambiguous", Matches: ms,
		Message: fmt.Sprintf("%q matches %d notes; use the path", ref, len(ms))}
}

// completeNotes completes note paths for the first argument.
func (a *app) completeNotes(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if err := a.openVault(false); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	_ = a.vault.Walk(func(e vault.Entry) error {
		if strings.HasPrefix(strings.ToLower(e.Path), strings.ToLower(prefix)) {
			out = append(out, e.Path)
		}
		return nil
	})
	return out, cobra.ShellCompDirectiveNoFileComp
}

// args validators that report usage errors with the exit code for them.
func exactArgs(n int, names string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != n {
			return usageErr("expected %s, got %d argument(s)", names, len(args))
		}
		return nil
	}
}

func minArgs(n int, names string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		if len(args) < n {
			return usageErr("expected %s", names)
		}
		return nil
	}
}
