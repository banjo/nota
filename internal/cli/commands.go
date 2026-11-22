package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/frontmatter"
	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/markdown"
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
	tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
	for _, e := range es {
		tags := ""
		for i, t := range e.Tags {
			if i > 0 {
				tags += " "
			}
			tags += "#" + t
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Path, e.Title, tags)
	}
	return tw.Flush()
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
the vault root.`,
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
			content, err := newNoteContent(text, tags, time.Now())
			if err != nil {
				return err
			}
			if _, err := a.index(); err != nil {
				return err
			}
			n, err := a.vault.Create(folder, content)
			if err != nil {
				return err
			}
			a.attrs = append(a.attrs, "path", n.Path)
			if !a.json {
				fmt.Fprintln(a.stdout, n.Path)
				return nil
			}
			e, err := a.ix.Get(n.Path)
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

// newNoteContent builds the file for `nota add`: the text, plus tags and a
// created timestamp in the frontmatter (added to frontmatter the text
// already has, if any).
func newNoteContent(text string, tags []string, now time.Time) ([]byte, error) {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	src := []byte(text)
	if len(tags) > 0 {
		props, _ := frontmatter.Split(src).Properties()
		list := tagList(props["tags"])
		for _, t := range tags {
			for _, one := range strings.Split(t, ",") {
				if c := markdown.CleanTag(one); c != "" {
					list = addTag(list, c)
				} else if strings.TrimSpace(one) != "" {
					return nil, usageErr("invalid tag %q", one)
				}
			}
		}
		var err error
		if src, err = frontmatter.Set(src, "tags", list); err != nil {
			return nil, err
		}
	}
	props, _ := frontmatter.Split(src).Properties()
	if _, ok := props["created"]; !ok {
		var err error
		if src, err = frontmatter.Set(src, "created", now.Format(time.RFC3339)); err != nil {
			return nil, err
		}
	}
	return src, nil
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
	return &cobra.Command{
		Use:   "open <note>",
		Short: "Open a note in $VISUAL or $EDITOR",
		Long: `Open a note in $VISUAL, else $EDITOR, else vi (notepad on Windows).
The editor value is split on spaces, so "code --wait" works.`,
		Args:              exactArgs(1, "a note"),
		ValidArgsFunction: a.completeNotes,
		RunE: func(_ *cobra.Command, args []string) error {
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
		Long:  `Move a note into a folder, keeping its file name. Use / for the vault root.`,
		Args:  exactArgs(2, "a note and a folder"),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
			return a.completeNotes(cmd, args, prefix)
		},
		RunE: func(_ *cobra.Command, args []string) error {
			p, err := a.resolve(args[0])
			if err != nil {
				return err
			}
			to, err := a.vault.Move(p, args[1])
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(map[string]string{"from": p, "path": to})
			}
			fmt.Fprintln(a.stdout, to)
			return nil
		},
	}
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
-foo removes (case-insensitive). Only the tags line changes; the rest of
the file stays byte for byte. Without changes it prints the note's tags.
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
			before := tagList(n.Props["tags"])
			after := append([]string{}, before...)
			for _, op := range rest {
				switch {
				case strings.HasPrefix(op, "-"):
					t := markdown.CleanTag(op[1:])
					if t == "" {
						return usageErr("invalid tag %q", op)
					}
					after = removeTag(after, t)
				default:
					t := markdown.CleanTag(strings.TrimPrefix(op, "+"))
					if t == "" {
						return usageErr("invalid tag %q", op)
					}
					after = addTag(after, t)
				}
			}
			if !equalStrings(before, after) {
				src := n.Raw
				if len(after) == 0 {
					src = frontmatter.Delete(src, "tags")
				} else if src, err = frontmatter.Set(src, "tags", after); err != nil {
					return err
				}
				if _, _, err := a.vault.Write(p, src); err != nil {
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

// tagList reads the tags property as Obsidian does: a list, or a string
// of comma or space separated tags.
func tagList(v any) []string {
	var raw []string
	switch x := v.(type) {
	case []any:
		for _, t := range x {
			raw = append(raw, fmt.Sprint(t))
		}
	case string:
		raw = strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == ' ' })
	}
	out := []string{}
	for _, t := range raw {
		if c := markdown.CleanTag(t); c != "" {
			out = addTag(out, c)
		}
	}
	return out
}

func addTag(ts []string, t string) []string {
	for _, x := range ts {
		if strings.EqualFold(x, t) {
			return ts
		}
	}
	return append(ts, t)
}

func removeTag(ts []string, t string) []string {
	out := ts[:0:0]
	for _, x := range ts {
		if !strings.EqualFold(x, t) {
			out = append(out, x)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (a *app) setCmd() *cobra.Command {
	var unset []string
	cmd := &cobra.Command{
		Use:   "set <note> key=value...",
		Short: "Set frontmatter properties",
		Long: `Set frontmatter properties. Values are YAML: numbers, true/false,
[lists] and {maps} are typed, anything else is a string. Only the edited
keys' lines change; everything else in the file stays byte for byte.
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
				if _, _, err := a.vault.Write(p, src); err != nil {
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

// parseValue decodes a CLI value as YAML when that gives a number, bool,
// list or map, and keeps it as the literal string otherwise (dates and
// text included, so "2026-11-22" is not turned into a timestamp).
func parseValue(s string) any {
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	switch v.(type) {
	case bool, int, int64, uint64, float64, []any, map[string]any:
		return v
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
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			for _, r := range rows {
				fmt.Fprintf(tw, "%s\t%s\t%d\n", r.ID, r.Name, r.Count)
			}
			return tw.Flush()
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
				fmt.Fprintf(a.stdout, "%s\t[[%s]]\n", l.From, l.Target)
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
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			for _, t := range ts {
				fmt.Fprintf(tw, "#%s\t%d\n", t.Tag, t.Count)
			}
			return tw.Flush()
		},
	}
}
