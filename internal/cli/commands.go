package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/edit"
	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/markdown"
	"github.com/banjo/nota/internal/plugins"
	"github.com/banjo/nota/internal/vault"
	"github.com/banjo/nota/internal/views"
)

// filterFlags are the selection flags shared by ls, inbox and search.
type filterFlags struct {
	folder string
	tag    string
	where  []string
	sort   string
	limit  int
}

func (f *filterFlags) register(cmd *cobra.Command, withSort bool, defaultSort string) {
	fl := cmd.Flags()
	fl.StringVar(&f.folder, "folder", "", "only notes in this folder and its subfolders")
	fl.StringVar(&f.tag, "tag", "", "only notes with this tag (nested tags included)")
	fl.StringArrayVar(&f.where, "where", nil, "only notes whose property matches `key=value` (or key!=value); repeatable")
	fl.IntVar(&f.limit, "limit", 0, "show at most this many notes")
	if withSort {
		fl.StringVar(&f.sort, "sort", defaultSort, "sort by path, title, created, updated or modified, optionally followed by asc or desc")
	}
}

// apply overlays the flags that were given onto q.
func (f *filterFlags) apply(cmd *cobra.Command, q *index.Query) error {
	fl := cmd.Flags()
	if fl.Changed("folder") {
		c, err := vault.CleanPath(f.folder)
		if err != nil {
			return err
		}
		q.Folder = c
	}
	if fl.Changed("tag") {
		q.Tag = f.tag
	}
	for _, w := range f.where {
		c, err := index.ParseCond(w)
		if err != nil {
			return usageErr("--where %q: want key=value or key!=value", w)
		}
		q.Where = append(q.Where, c)
	}
	if fl.Lookup("sort") != nil && (fl.Changed("sort") || q.Sort.Field == "") {
		s, err := index.ParseSort(f.sort)
		if err != nil {
			return usageErr("--sort %q: want path, title, created, updated or modified, then asc or desc", f.sort)
		}
		q.Sort = s
	}
	if fl.Changed("limit") {
		q.Limit = f.limit
	}
	return nil
}

func (a *app) printEntries(es []index.Entry) error {
	if a.json {
		return a.printJSON(es)
	}
	rows := make([][]string, len(es))
	for i, e := range es {
		rows[i] = []string{e.Path, e.Title, hashTags(e.Tags)}
	}
	return table(a.stdout, rows)
}

func (a *app) addCmd() *cobra.Command {
	var folder string
	var tags []string
	cmd := &cobra.Command{
		Use:   "add [text...|-]",
		Short: "Create a note (in the inbox by default)",
		Long: `Create a note. The first line becomes its title and file name.
With "-" (or no text and piped input) the note is read from stdin.
Notes go to the inbox folder unless --folder is given; use --folder / for
the vault root. The printed path is where the note ended up, after any
transform hooks (.nota/hooks.toml) filed it.`,
		Example: `  nota add "Köp mjölk" --tag handla
  nota add --folder Projekt "# Plan" 
  pbpaste | nota add -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := a.addText(args)
			if err != nil {
				return err
			}
			if err := a.openVault(true); err != nil {
				return err
			}
			if !cmd.Flags().Changed("folder") {
				folder = a.vault.Inbox()
			}
			content, err := edit.New(text, tags, nil, a.vault.Now())
			if err != nil {
				return err
			}
			if _, err := a.index(); err != nil {
				return err
			}
			where, stop := a.bus.Follow() // a transform hook may file the note
			n, err := a.vault.Create(folder, content)
			stop()
			if err != nil {
				return err
			}
			p := where(n.Path)
			a.attrs = append(a.attrs, "path", p)
			if !a.json {
				fmt.Fprintln(a.stdout, p)
				return nil
			}
			e, err := a.ix.Get(p)
			if err != nil {
				return err
			}
			return a.printJSON(e)
		},
	}
	cmd.Flags().StringVar(&folder, "folder", "", "folder for the note (default: the inbox)")
	cmd.Flags().StringArrayVarP(&tags, "tag", "t", nil, "add a tag; repeatable")
	return cmd
}

// addText collects the note text from the arguments or stdin.
func (a *app) addText(args []string) (string, error) {
	fromStdin := len(args) == 1 && args[0] == "-"
	if len(args) == 0 {
		if f, ok := a.stdin.(*os.File); ok {
			if st, err := f.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
				return "", usageErr("nothing to add: give the text as arguments or pipe it in with -")
			}
		}
		fromStdin = true
	}
	if !fromStdin {
		return strings.Join(args, " "), nil
	}
	b, err := io.ReadAll(a.stdin)
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return "", usageErr("nothing to add: stdin was empty")
	}
	return string(b), nil
}

func (a *app) lsCmd() *cobra.Command {
	var f filterFlags
	var view string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List notes",
		Example: `  nota ls --folder Projekt
  nota ls --tag projekt --where status=draft --sort "updated desc"
  nota ls --view drafts --json`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ix, err := a.index()
			if err != nil {
				return err
			}
			q := index.Query{}
			if view != "" {
				vs, err := views.Load(a.vault)
				if err != nil {
					return err
				}
				vw, ok := views.Find(vs, view)
				if !ok {
					return &Error{Code: ExitNotFound, Kind: "not_found", Message: fmt.Sprintf("no view %q; see `nota views`", view)}
				}
				if q, err = vw.Query(); err != nil {
					return err
				}
			}
			if err := f.apply(cmd, &q); err != nil {
				return err
			}
			es, err := ix.Query(q)
			if err != nil {
				return err
			}
			a.attrs = append(a.attrs, "results", len(es))
			return a.printEntries(es)
		},
	}
	f.register(cmd, true, "path")
	cmd.Flags().StringVar(&view, "view", "", "start from a saved view (.nota/views/NAME.yaml)")
	return cmd
}

func (a *app) inboxCmd() *cobra.Command {
	var f filterFlags
	cmd := &cobra.Command{
		Use:   "inbox",
		Short: "List unsorted notes in the inbox, newest first",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ix, err := a.index()
			if err != nil {
				return err
			}
			q := index.Query{Folder: a.vault.Inbox()}
			if err := f.apply(cmd, &q); err != nil {
				return err
			}
			es, err := ix.Query(q)
			if err != nil {
				return err
			}
			return a.printEntries(es)
		},
	}
	f.register(cmd, true, "created desc")
	cmd.Flags().MarkHidden("folder")
	return cmd
}

func (a *app) searchCmd() *cobra.Command {
	var f filterFlags
	cmd := &cobra.Command{
		Use:   "search <query...>",
		Short: "Full-text search over titles and bodies",
		Long: `Search notes. All words must occur (the last one as a prefix);
"quoted phrases" match exactly, -word excludes, OR between words means
either. Best matches first; title matches weigh more.`,
		Args: minArgs(1, "a search query"),
		RunE: func(cmd *cobra.Command, args []string) error {
			ix, err := a.index()
			if err != nil {
				return err
			}
			q := index.Query{Limit: 50}
			if err := f.apply(cmd, &q); err != nil {
				return err
			}
			es, err := ix.Search(strings.Join(args, " "), q)
			if err != nil {
				return err
			}
			a.attrs = append(a.attrs, "results", len(es))
			if a.json {
				return a.printJSON(es)
			}
			for _, e := range es {
				fmt.Fprintf(a.stdout, "%s\t%s\n", e.Path, e.Title)
				if s := strings.Join(strings.Fields(e.Snippet), " "); s != "" {
					fmt.Fprintf(a.stdout, "    %s\n", s)
				}
			}
			return nil
		},
	}
	f.register(cmd, false, "")
	return cmd
}

// noteJSON is a note with its content, for cat --json.
type noteJSON struct {
	index.Entry
	Body    string `json:"body"`
	Content string `json:"content"`
}

func (a *app) catCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "cat <note>",
		Short:             "Print a note",
		Args:              exactArgs(1, "a note"),
		ValidArgsFunction: a.completeNotes,
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := a.resolve(args[0])
			if err != nil {
				return err
			}
			n, err := a.vault.Read(p)
			if err != nil {
				return err
			}
			if !a.json {
				_, err := a.stdout.Write(n.Raw)
				return err
			}
			ix, err := a.index()
			if err != nil {
				return err
			}
			e, err := ix.Get(p)
			if err != nil {
				// An ignored note can be read by path but is not indexed.
				e = index.Entry{Path: n.Path, Folder: n.Folder(), Title: n.Title, Props: n.Props, Tags: []string{}, Modified: n.ModTime, Size: n.Size}
			}
			return a.printJSON(noteJSON{Entry: e, Body: n.Body, Content: string(n.Raw)})
		},
	}
}

func (a *app) openCmd() *cobra.Command {
	var inApp bool
	cmd := &cobra.Command{
		Use:   "open <note>",
		Short: "Open a note in $VISUAL or $EDITOR, or in the Nota app",
		Long: `Open a note in $VISUAL, else $EDITOR, else vi (notepad on Windows).
The editor value is split on spaces, so "code --wait" works.

With --app, open it in the Nota desktop app instead: a note of the vault
opens as that note, in Reading mode. --app also takes any Markdown file
(.md, .markdown) by its path, in the vault or not; a file outside the
vault opens as a read-only preview and is never changed or copied unless
you choose to in the app. On macOS this goes through Launch Services
(open -b ` + appBundleID + `), on Linux through ` + linuxApp + `; $` + EnvApp + ` overrides it.`,
		Args:              exactArgs(1, "a note"),
		ValidArgsFunction: a.completeNotes,
		RunE: func(_ *cobra.Command, args []string) error {
			if inApp {
				return a.openInApp(args[0])
			}
			p, err := a.resolve(args[0])
			if err != nil {
				return err
			}
			disk, err := a.vault.DiskPath(p)
			if err != nil {
				return err
			}
			editor := strings.Fields(firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"), defaultEditor()))
			c := exec.Command(editor[0], append(editor[1:], disk)...)
			c.Stdin, c.Stdout, c.Stderr = a.stdin, a.stdout, a.stderr
			if a.json {
				c.Stdout = a.stderr // keep stdout valid JSON
			}
			a.log.Debug("editor", "cmd", c.Args)
			if err := c.Run(); err != nil {
				return fmt.Errorf("editor %s: %w", editor[0], err)
			}
			if a.ix != nil {
				if err := a.ix.Update(a.vault, p); err != nil {
					return err
				}
			}
			if a.json {
				return a.printJSON(map[string]string{"path": p})
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&inApp, "app", "a", false, "open in the Nota desktop app (a note, or any Markdown file by path)")
	return cmd
}

func defaultEditor() string {
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func (a *app) mvCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mv <note> <folder>",
		Short: "Move a note to another folder",
		Long: `Move a note into a folder, keeping its file name. Use / for the vault root.
The file's content is not touched, so its "updated" timestamp stays.`,
		Args: exactArgs(2, "a note and a folder"),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
			return a.completeNotes(cmd, args, prefix)
		},
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := a.resolve(args[0])
			if err != nil {
				return err
			}
			where, stop := a.bus.Follow() // moving into the inbox runs inbox.added hooks
			to, err := a.vault.Move(p, args[1])
			stop()
			if err != nil {
				return err
			}
			to = where(to)
			if a.json {
				return a.printJSON(map[string]string{"from": p, "path": to})
			}
			fmt.Fprintln(a.stdout, to)
			return nil
		},
	}
}

func (a *app) folderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "folder",
		Short: "Make, list and rename folders",
		Long: `Folders are real directories in the vault. "nota folder add" makes empty ones,
which stay local until a note lands in them (see docs/sync.md); notes are moved
into folders with "nota mv", and folders renamed with "nota folder rename".`,
	}
	add := &cobra.Command{
		Use:   "add <path>...",
		Short: "Create empty folders (nested paths like a/b make the parents too)",
		Example: `  nota folder add Projects/Ideas
  nota folder add --json Archive/2026`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := a.openVault(false); err != nil {
				return err
			}
			made := []string{}
			for _, arg := range args {
				p, err := a.vault.Mkdir(arg)
				if err != nil {
					return err
				}
				made = append(made, p)
			}
			a.attrs = append(a.attrs, "folders", len(made))
			if a.json {
				if len(made) == 1 {
					return a.printJSON(map[string]string{"path": made[0]})
				}
				return a.printJSON(map[string][]string{"paths": made})
			}
			for _, p := range made {
				fmt.Fprintln(a.stdout, p)
			}
			return nil
		},
	}
	ls := &cobra.Command{
		Use:   "ls",
		Short: "List every folder, empty ones too, with its note count",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			ix, err := a.index()
			if err != nil {
				return err
			}
			fs, err := ix.Folders(a.vault)
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(fs)
			}
			rows := make([][]string, len(fs))
			for i, f := range fs {
				rows[i] = []string{f.Path, strconv.Itoa(f.Notes)}
			}
			return table(a.stdout, rows)
		},
	}
	rename := &cobra.Command{
		Use:   "rename <folder> <new-path>",
		Short: "Rename a folder, or move it under another one",
		Long: `Rename a folder by moving every file in it to the new path, so each note
moves like with "nota mv" (links name notes, not folders, so none change).
The new path is a full vault path: nota folder rename Work Projects, or
nota folder rename Work Archive/2026 to move it as well.`,
		Example: `  nota folder rename Projects Work
  nota folder rename --json Inbox/Old Archive/Old`,
		Args: exactArgs(2, "a folder and its new path"),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := a.openVault(false); err != nil {
				return err
			}
			from, err := vault.CleanPath(args[0])
			if err != nil {
				return err
			}
			to, err := a.vault.RenameFolder(from, args[1])
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(map[string]string{"from": from, "path": to})
			}
			fmt.Fprintln(a.stdout, to)
			return nil
		},
	}
	cmd.AddCommand(add, ls, rename)
	return cmd
}

func (a *app) rmCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "rm <note>",
		Short:             "Move a note to the vault's .trash/",
		Args:              exactArgs(1, "a note"),
		ValidArgsFunction: a.completeNotes,
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := a.resolve(args[0])
			if err != nil {
				return err
			}
			trash, err := a.vault.Delete(p)
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(map[string]string{"path": p, "trash": trash})
			}
			fmt.Fprintln(a.stdout, trash)
			return nil
		},
	}
}

func (a *app) tagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tag <note> [+tag|-tag|tag]...",
		Short: "Add or remove tags in a note's frontmatter",
		Long: `Edit the "tags" list in a note's frontmatter: +foo or foo adds,
-foo removes (case-insensitive). Only the tags line (and "updated", when
the note has frontmatter) changes; the rest of the file stays byte for
byte. Without changes it prints the note's tags.

-foo only edits the frontmatter: inline #foo tags in the body are left
alone, so the note may still show up under #foo. Edit the body to drop
those.

Flags must come before the note: nota tag --json Plan +a -b.`,
		Args:              minArgs(1, "a note"),
		ValidArgsFunction: a.completeNotes,
		RunE: func(_ *cobra.Command, args []string) error {
			// Flags after the note are taken literally (so -tag works), except --json.
			var rest []string
			for _, s := range args[1:] {
				if s == "--json" {
					a.json = true
					continue
				}
				rest = append(rest, s)
			}
			p, err := a.resolve(args[0])
			if err != nil {
				return err
			}
			n, err := a.vault.Read(p)
			if err != nil {
				return err
			}
			if err := checkFrontmatter(n); err != nil {
				return err
			}
			before := edit.TagList(n.Props["tags"])
			after := append([]string{}, before...)
			for _, op := range rest {
				switch {
				case strings.HasPrefix(op, "-"):
					t := markdown.CleanTag(op[1:])
					if t == "" {
						return usageErr("invalid tag %q", op)
					}
					after = edit.RemoveTag(after, t)
				default:
					t := markdown.CleanTag(strings.TrimPrefix(op, "+"))
					if t == "" {
						return usageErr("invalid tag %q", op)
					}
					after = edit.AddTag(after, t)
				}
			}
			if !slices.Equal(before, after) {
				src := n.Raw
				if len(after) == 0 {
					src = frontmatter.Delete(src, "tags")
				} else if src, err = frontmatter.Set(src, "tags", after); err != nil {
					return err
				}
				if _, _, err := a.vault.Save(p, src); err != nil {
					return err
				}
			}
			if a.json {
				return a.printJSON(map[string]any{"path": p, "tags": after})
			}
			fmt.Fprintf(a.stdout, "%s\t%s\n", p, hashTags(after))
			return nil
		},
	}
	cmd.Flags().SetInterspersed(false)
	return cmd
}

// table prints rows as columns separated by two spaces, without trailing
// white space (so an empty last column leaves no padding behind).
func table(w io.Writer, rows [][]string) error {
	var widths []int
	for _, r := range rows {
		for i, c := range r {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], utf8.RuneCountInString(c))
		}
	}
	var b strings.Builder
	for _, r := range rows {
		line := ""
		for i, c := range r {
			if i < len(r)-1 {
				c += strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c)+2)
			}
			line += c
		}
		b.WriteString(strings.TrimRight(line, " "))
		b.WriteByte('\n')
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func hashTags(ts []string) string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = "#" + t
	}
	return strings.Join(out, " ")
}

// checkFrontmatter refuses to edit a note whose frontmatter does not parse:
// a line-based edit could make it worse, and the user should see why.
func checkFrontmatter(n *vault.Note) error {
	if _, err := frontmatter.Split(n.Raw).Properties(); err != nil {
		return fmt.Errorf("%s: frontmatter is not valid YAML, fix it first: %w", n.Path, err)
	}
	return nil
}

func (a *app) setCmd() *cobra.Command {
	var unset []string
	cmd := &cobra.Command{
		Use:   "set <note> key=value...",
		Short: "Set frontmatter properties",
		Long: `Set frontmatter properties. Values are YAML: numbers, true/false,
[lists] and {maps} are typed, anything else is a string. Only the edited
keys' lines change, plus "updated" (set to now) when the note already had
frontmatter; everything else in the file stays byte for byte.
--unset removes a key.`,
		Example: `  nota set Plan status=done priority=2
  nota set Plan "aliases=[plan, planen]" --unset draft`,
		Args:              minArgs(1, "a note and key=value pairs"),
		ValidArgsFunction: a.completeNotes,
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 1 && len(unset) == 0 {
				return usageErr("nothing to set: give key=value pairs or --unset key")
			}
			type kv struct {
				key string
				val any
			}
			var sets []kv
			for _, s := range args[1:] {
				i := strings.IndexByte(s, '=')
				if i <= 0 {
					return usageErr("%q is not key=value", s)
				}
				k := strings.TrimSpace(s[:i])
				if strings.ContainsAny(k, "\n\r") {
					return usageErr("invalid key %q", k)
				}
				sets = append(sets, kv{k, parseValue(s[i+1:])})
			}
			p, err := a.resolve(args[0])
			if err != nil {
				return err
			}
			n, err := a.vault.Read(p)
			if err != nil {
				return err
			}
			if err := checkFrontmatter(n); err != nil {
				return err
			}
			src := n.Raw
			for _, s := range sets {
				if src, err = frontmatter.Set(src, s.key, s.val); err != nil {
					return fmt.Errorf("set %s: %w", s.key, err)
				}
			}
			for _, k := range unset {
				src = frontmatter.Delete(src, k)
			}
			if !bytes.Equal(src, n.Raw) {
				if _, _, err := a.vault.Save(p, src); err != nil {
					return err
				}
			}
			if !a.json {
				fmt.Fprintln(a.stdout, p)
				return nil
			}
			props, err := frontmatter.Split(src).Properties()
			if err != nil {
				return err
			}
			return a.printJSON(map[string]any{"path": p, "properties": props})
		},
	}
	cmd.Flags().StringArrayVar(&unset, "unset", nil, "remove this `key`; repeatable")
	return cmd
}

// parseValue decodes a CLI value as YAML when that gives a number, a bool,
// a [flow list] or a {flow map}, and keeps it as the literal string
// otherwise (dates and text included, so "2026-11-22" stays a date string).
func parseValue(s string) any {
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	t := strings.TrimSpace(s)
	switch v.(type) {
	case bool, int, int64, uint64, float64:
		return v
	case []any:
		if strings.HasPrefix(t, "[") { // not "- a", which is a list too
			return v
		}
	case map[string]any:
		if strings.HasPrefix(t, "{") { // not "a: b", which is a map too
			return v
		}
	}
	return s
}

func (a *app) indexCmd() *cobra.Command {
	var rebuild bool
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Update the search index (or --rebuild it from scratch)",
		Long: `Bring the index in .nota/index.db up to date with the files. Every
command does this on its own; run it after big outside changes or with
--rebuild to drop the index and build it again.`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := a.openVault(false); err != nil {
				return err
			}
			ix, err := index.OpenVault(a.vault)
			if err != nil {
				return err
			}
			a.ix = ix
			var st index.Stats
			if rebuild {
				st, err = ix.Rebuild(a.vault)
			} else {
				st, err = ix.Sync(a.vault)
			}
			if err != nil {
				return err
			}
			a.attrs = append(a.attrs, "rebuild", rebuild, "notes", st.Notes)
			if a.json {
				return a.printJSON(struct {
					index.Stats
					Rebuilt bool    `json:"rebuilt"`
					Seconds float64 `json:"seconds"`
				}{st, rebuild || ix.Recreated, st.Duration.Seconds()})
			}
			fmt.Fprintf(a.stdout, "%d notes: %d added, %d updated, %d removed (%v)\n",
				st.Notes, st.Added, st.Updated, st.Removed, st.Duration.Round(time.Millisecond))
			return nil
		},
	}
	cmd.Flags().BoolVar(&rebuild, "rebuild", false, "drop the index and rebuild it from the files")
	return cmd
}

func (a *app) viewsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "views",
		Short: "List saved views (.nota/views/*.yaml) with their note counts",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			ix, err := a.index()
			if err != nil {
				return err
			}
			vs, err := views.Load(a.vault)
			if err != nil {
				return err
			}
			type row struct {
				views.View
				Count int `json:"count"`
			}
			rows := []row{}
			for _, vw := range vs {
				es, err := views.Eval(ix, vw)
				if err != nil {
					return err
				}
				rows = append(rows, row{vw, len(es)})
			}
			if a.json {
				return a.printJSON(rows)
			}
			out := make([][]string, len(rows))
			for i, r := range rows {
				out[i] = []string{r.ID, r.Name, strconv.Itoa(r.Count)}
			}
			return table(a.stdout, out)
		},
	}
}

func (a *app) pluginsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "plugins",
		Short: "List the vault's plugins (.nota/plugins) and their themes; exit 1 if one is broken",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := a.openVault(false); err != nil {
				return err
			}
			ps, err := plugins.Load(a.vault)
			if err != nil {
				return err
			}
			broken := 0
			rows := make([][]string, len(ps))
			for i, p := range ps {
				what := ""
				if p.Error != "" {
					broken++
					what = "error: " + p.Error
				} else {
					names := make([]string, len(p.Contributes.Themes))
					for j, t := range p.Contributes.Themes {
						names[j] = t.Name
					}
					what = "themes: " + strings.Join(names, ", ")
				}
				rows[i] = []string{p.ID, p.Dir, what}
			}
			if a.json {
				err = a.printJSON(ps)
			} else {
				err = table(a.stdout, rows)
			}
			if err != nil {
				return err
			}
			if broken > 0 {
				return &Error{Code: ExitError, Kind: "broken_plugin", Message: fmt.Sprintf("%d of %d plugins did not load", broken, len(ps))}
			}
			return nil
		},
	}
}

func (a *app) backlinksCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "backlinks <note>",
		Short:             "List the notes that link to a note",
		Args:              exactArgs(1, "a note"),
		ValidArgsFunction: a.completeNotes,
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := a.resolve(args[0])
			if err != nil {
				return err
			}
			ix, err := a.index()
			if err != nil {
				return err
			}
			ls, err := ix.Backlinks(p)
			if errors.Is(err, vault.ErrNotFound) {
				ls, err = []index.Link{}, nil // an ignored note has no indexed links
			}
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(ls)
			}
			for _, l := range ls {
				bang := ""
				if l.Embed {
					bang = "!"
				}
				fmt.Fprintf(a.stdout, "%s\t%s[[%s]]\n", l.From, bang, l.Target)
			}
			return nil
		},
	}
}

func (a *app) tagsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tags",
		Short: "List all tags with their note counts",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			ix, err := a.index()
			if err != nil {
				return err
			}
			ts, err := ix.Tags()
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(ts)
			}
			rows := make([][]string, len(ts))
			for i, t := range ts {
				rows[i] = []string{"#" + t.Tag, strconv.Itoa(t.Count)}
			}
			return table(a.stdout, rows)
		},
	}
}
