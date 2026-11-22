package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/events"
	"github.com/banjo/nota/internal/hooks"
)

func (a *app) hooksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "List and try the vault's hooks (.nota/hooks.toml)",
		Long: `Hooks are scripts that run when notes change, configured in
.nota/hooks.toml:

  [[hook]]
  event = "inbox.added"      # note.created, note.saved, note.moved,
                             # note.deleted, inbox.added, sync.completed
  run = "sh scripts/file.sh" # run with the vault as working directory
  mode = "transform"         # "after" (default) or "transform"
  timeout = "2s"             # default 2s for transform, 30s for after
  match = { folder = "Inbox", tag = "jobb" }  # optional

A hook reads the event as JSON on stdin (event, vault, path, title, tags,
frontmatter, body) and gets $NOTA_VAULT, $NOTA_EVENT, $NOTA_NOTE and
$NOTA_HOOK. After hooks run in the background (nota waits for them before
it exits). Transform hooks run only on note.created and inbox.added and
print a patch on stdout:

  {"set": {"status": "todo"}, "unset": ["draft"],
   "tags": {"add": ["jobb"], "remove": []}, "folder": "Jobb"}

The patch is applied like any other edit (events fire, "updated" is
set), but never runs the same hook again. A failing, slow or invalid hook
is logged and leaves the note unchanged.`,
	}
	cmd.AddCommand(a.hooksListCmd(), a.hooksTestCmd())
	return cmd
}

func (a *app) loadHooks() (*hooks.Runner, error) {
	if err := a.openVault(false); err != nil {
		return nil, err
	}
	cfg, err := hooks.Load(a.vault)
	if err != nil {
		return nil, err
	}
	return hooks.New(a.vault, cfg, hooks.Options{Logger: a.log}), nil
}

func (a *app) hooksListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the configured hooks",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			r, err := a.loadHooks()
			if err != nil {
				return err
			}
			type row struct {
				hooks.Hook
				Timeout string `json:"timeout"`
			}
			rows := []row{}
			for _, h := range r.Hooks() {
				rows = append(rows, row{h, h.Timeout.String()})
			}
			if a.json {
				return a.printJSON(rows)
			}
			out := make([][]string, len(rows))
			for i, r := range rows {
				var m []string
				if r.Match.Folder != "" {
					m = append(m, "folder="+r.Match.Folder)
				}
				if r.Match.Tag != "" {
					m = append(m, "tag="+r.Match.Tag)
				}
				out[i] = []string{r.ID, string(r.Event), string(r.Mode), r.Timeout, strings.Join(m, ","), r.Run}
			}
			return table(a.stdout, out)
		},
	}
}

func (a *app) hooksTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test <event> <note>",
		Short: "Run the hooks for an event on a note, without applying anything",
		Long: `Run every hook configured for event on the note, as if the event had
just happened, and show what each one printed. Transform patches are
parsed and shown but not applied, so this is safe to repeat.`,
		Example: `  nota hooks test inbox.added "Inbox/Ring Anna"`,
		Args:    exactArgs(2, "an event and a note"),
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				out := make([]string, len(events.Types))
				for i, t := range events.Types {
					out[i] = string(t)
				}
				return out, cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(_ *cobra.Command, args []string) error {
			ev := events.Type(args[0])
			if !slices.Contains(events.Types, ev) {
				return usageErr("unknown event %q", args[0])
			}
			r, err := a.loadHooks()
			if err != nil {
				return err
			}
			p, err := a.resolve(args[1])
			if err != nil {
				return err
			}
			res, err := r.Test(ev, p)
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(res)
			}
			if len(res) == 0 {
				fmt.Fprintf(a.stdout, "no hooks for %s on %s\n", ev, p)
				return nil
			}
			for _, x := range res {
				status := "ok"
				if x.Error != "" {
					status = "error: " + x.Error
				}
				fmt.Fprintf(a.stdout, "%s (%s, %.2fs): %s\n", x.Hook, x.Mode, x.Seconds, status)
				if x.Patch != nil {
					if x.Patch.Empty() {
						fmt.Fprintln(a.stdout, "  patch: no change")
					} else {
						fmt.Fprintf(a.stdout, "  patch: %s\n", patchString(*x.Patch))
					}
				} else if s := strings.TrimSpace(x.Output); s != "" {
					fmt.Fprintf(a.stdout, "  output: %s\n", s)
				}
			}
			return nil
		},
	}
}

func patchString(p hooks.Patch) string {
	var parts []string
	if p.Folder != "" {
		parts = append(parts, "folder="+p.Folder)
	}
	for _, t := range p.Tags.Add {
		parts = append(parts, "+"+t)
	}
	for _, t := range p.Tags.Remove {
		parts = append(parts, "-"+t)
	}
	keys := make([]string, 0, len(p.Set))
	for k := range p.Set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, p.Set[k]))
	}
	for _, k := range p.Unset {
		parts = append(parts, "unset "+k)
	}
	return strings.Join(parts, " ")
}
