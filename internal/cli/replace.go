package cli

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/index"
	"github.com/banjo/nota/internal/replace"
)

func (a *app) replaceCmd() *cobra.Command {
	var f filterFlags
	var o replace.Options
	var dry, yes bool
	cmd := &cobra.Command{
		Use:   "replace <find> <replace>",
		Short: "Find and replace text across notes",
		Long: `Find and replace text in the bodies of notes; frontmatter is never
touched (use nota set and nota tag for that). Code blocks and inline code
are skipped unless --code is given. The search ignores case unless --case;
--word matches whole words only; --regex reads <find> as a regular
expression (RE2 syntax), and in <replace> $1 is its first group (also in
"$1st"), ${name} a named group and $$ a dollar sign. Link targets are
left alone: rename the note instead (nota mv relinks).

Every change is listed first, as [-old-]{+new+} on its line, and nota asks
before writing. --yes writes without asking, --dry-run only lists. Each
changed note is saved as an edit: "updated" moves, and hooks and sync see
it.`,
		Example: `  nota replace "Anna Svensson" "Anna Berg"
  nota replace --word --folder Projekt kund klient --yes
  nota replace --regex '(\d{4})-(\d{2})-(\d{2})' '$3/$2/$1' --dry-run
  nota replace TODO DONE --tag sprint --json --dry-run`,
		Args: exactArgs(2, "what to find and what to replace it with"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dry && yes {
				return usageErr("--dry-run and --yes do not go together")
			}
			if a.json && !dry && !yes {
				return usageErr("with --json, pass --yes to replace or --dry-run to list")
			}
			o.Find, o.Replace = args[0], args[1]
			r, err := replace.New(o)
			if err != nil {
				return usageErr("%v", err)
			}
			ix, err := a.index()
			if err != nil {
				return err
			}
			q := index.Query{Sort: index.Sort{Field: "path"}}
			if err := f.apply(cmd, &q); err != nil {
				return err
			}
			notes, err := replace.Find(a.vault, ix, r, q)
			if err != nil {
				return err
			}
			total := 0
			for _, n := range notes {
				total += len(n.Matches)
			}
			a.attrs = append(a.attrs, "notes", len(notes), "matches", total)
			if a.json && dry {
				return a.printJSON(notes)
			}
			if !a.json {
				a.printMatches(notes)
			}
			if dry || total == 0 {
				if total == 0 && !a.json {
					fmt.Fprintln(a.stderr, "nothing matches")
				}
				return nil
			}
			if !yes {
				fmt.Fprintf(a.stderr, "Replace %s in %s? [y/N] ", matches(total), plural(len(notes), "note"))
				line, _ := bufio.NewReader(a.stdin).ReadString('\n')
				if ans := strings.ToLower(strings.TrimSpace(line)); ans != "y" && ans != "yes" {
					fmt.Fprintln(a.stderr, "nothing changed")
					return nil
				}
			}
			picks := make([]replace.Pick, len(notes))
			for i, n := range notes {
				picks[i] = replace.Pick{Path: n.Path, Hash: n.Hash}
			}
			res, err := replace.ApplyNotes(a.vault, r, picks)
			if err == nil && len(res.Stale) > 0 {
				err = fmt.Errorf("changed while replacing, left alone: %s", strings.Join(res.Stale, ", "))
			}
			if a.json {
				type changed struct {
					Path  string `json:"path"`
					Count int    `json:"count"`
				}
				out := struct {
					Notes []changed `json:"notes"`
					Count int       `json:"count"`
				}{Notes: []changed{}, Count: res.Count}
				for _, c := range res.Notes {
					out.Notes = append(out.Notes, changed{c.Path, c.Count})
				}
				if perr := a.printJSON(out); perr != nil {
					return perr
				}
			} else if res.Count > 0 {
				fmt.Fprintf(a.stderr, "Replaced %s in %s\n", matches(res.Count), plural(len(res.Notes), "note"))
			}
			return err
		},
	}
	f.register(cmd, false, "")
	fl := cmd.Flags()
	fl.BoolVar(&o.Regex, "regex", false, "read <find> as a regular expression; $1 in <replace> is its first group")
	fl.BoolVar(&o.Case, "case", false, "match case")
	fl.BoolVar(&o.Word, "word", false, "match whole words only")
	fl.BoolVar(&o.Code, "code", false, "also replace in code blocks and inline code")
	fl.BoolVar(&dry, "dry-run", false, "list the changes, write nothing")
	fl.BoolVar(&yes, "yes", false, "replace without asking")
	return cmd
}

// printMatches lists each match on its line as [-old-]{+new+}.
func (a *app) printMatches(notes []replace.Note) {
	for _, n := range notes {
		fmt.Fprintln(a.stdout, n.Path)
		for _, m := range n.Matches {
			fmt.Fprintf(a.stdout, "  %d: %s[-%s-]{+%s+}%s\n", m.Line, m.Before, m.Text, m.With, m.After)
		}
	}
}

func matches(n int) string {
	if n == 1 {
		return "1 match"
	}
	return fmt.Sprintf("%d matches", n)
}
