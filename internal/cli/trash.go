package cli

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/vault"
)

func (a *app) trashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "List, restore and empty the vault's .trash/",
		Long: `Deleted notes go to the vault's .trash/ ("nota rm", the app, and deletes
synced from other devices). These commands list them, put them back and
delete them for good.

A note in the trash is named by its path there (".trash/Projekt/Plan.md"
or "Projekt/Plan.md") or by the path it was deleted from; for a path
deleted more than once that is the newest delete.`,
	}
	ls := &cobra.Command{
		Use:   "ls",
		Short: "List what's in the trash, most recently deleted first",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := a.openVault(false); err != nil {
				return err
			}
			items, err := a.vault.Trash()
			if err != nil {
				return err
			}
			a.attrs = append(a.attrs, "items", len(items))
			if a.json {
				if items == nil {
					items = []vault.TrashItem{}
				}
				return a.printJSON(items)
			}
			if len(items) == 0 {
				fmt.Fprintln(a.stderr, "the trash is empty")
				return nil
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "DELETED\tFROM\tIN TRASH")
			for _, it := range items {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", it.DeletedAt.Local().Format(time.RFC3339), it.From, it.Path)
			}
			return tw.Flush()
		},
	}
	var to string
	restore := &cobra.Command{
		Use:   "restore <note> [--to <folder>]",
		Short: "Put a note back where it was deleted from, or in another folder",
		Long: `Move a note out of the trash, back to the path it was deleted from.
--to puts it in another folder instead, or at another path when it ends
in the note's extension. It refuses to overwrite a note that has taken
the path since.`,
		Args:              exactArgs(1, "a note in the trash"),
		ValidArgsFunction: a.completeTrash,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := a.openVault(false); err != nil {
				return err
			}
			it, err := a.vault.FindTrash(args[0])
			if err != nil {
				return err
			}
			dst := ""
			if to != "" {
				dst = to
				if !strings.EqualFold(path.Ext(to), path.Ext(it.From)) {
					dst = path.Join(to, path.Base(it.From))
				}
			}
			p, err := a.vault.RestoreTrash(it.Path, dst)
			if errors.Is(err, vault.ErrExists) || errors.Is(err, vault.ErrCaseCollision) {
				e := classify(err)
				e.Message += "; restore it elsewhere with --to <folder> or --to <new name>.md"
				return e
			}
			if err != nil {
				return err
			}
			if a.json {
				return a.printJSON(map[string]string{"path": p, "trash": it.Path})
			}
			fmt.Fprintln(a.stdout, p)
			return nil
		},
	}
	restore.Flags().StringVar(&to, "to", "", "folder (or new path) to restore into")
	var yes bool
	rm := &cobra.Command{
		Use:               "rm <note> --yes",
		Short:             "Delete a note in the trash for good",
		Args:              exactArgs(1, "a note in the trash"),
		ValidArgsFunction: a.completeTrash,
		RunE: func(_ *cobra.Command, args []string) error {
			if err := a.openVault(false); err != nil {
				return err
			}
			it, err := a.vault.FindTrash(args[0])
			if err != nil {
				return err
			}
			if !yes {
				return usageErr("this deletes %s for good; pass --yes", it.Path)
			}
			if err := a.vault.PurgeTrash(it.Path); err != nil {
				return err
			}
			if a.json {
				return a.printJSON(map[string]string{"trash": it.Path})
			}
			fmt.Fprintln(a.stdout, it.Path)
			return nil
		},
	}
	rm.Flags().BoolVar(&yes, "yes", false, "really delete")
	var older string
	var yesEmpty bool
	empty := &cobra.Command{
		Use:   "empty [--older <age>] --yes",
		Short: "Delete everything in the trash for good",
		Long: `Delete every note in the trash for good, or with --older only those
deleted longer ago than that: 30d, 2w, 12h, 90m.`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := a.openVault(false); err != nil {
				return err
			}
			var cutoff time.Time
			if older != "" {
				d, err := parseAge(older)
				if err != nil {
					return usageErr("--older: %v", err)
				}
				cutoff = a.vault.Now().Add(-d)
			}
			if !yesEmpty {
				return usageErr("this deletes the notes in the trash for good; pass --yes")
			}
			n, err := a.vault.EmptyTrash(cutoff)
			if err != nil {
				return err
			}
			a.attrs = append(a.attrs, "deleted", n)
			if a.json {
				return a.printJSON(map[string]int{"deleted": n})
			}
			fmt.Fprintf(a.stderr, "deleted %s for good\n", plural(n, "file"))
			return nil
		},
	}
	empty.Flags().StringVar(&older, "older", "", "only what was deleted longer ago than this (30d, 2w, 12h)")
	empty.Flags().BoolVar(&yesEmpty, "yes", false, "really delete")
	cmd.AddCommand(ls, restore, rm, empty)
	return cmd
}

// parseAge parses a duration with days and weeks too: 30d, 2w, 12h, 90m.
func parseAge(s string) (time.Duration, error) {
	for unit, d := range map[string]time.Duration{"d": 24 * time.Hour, "w": 7 * 24 * time.Hour} {
		if n, ok := strings.CutSuffix(s, unit); ok {
			v, err := strconv.ParseFloat(n, 64)
			if err != nil || v < 0 {
				return 0, fmt.Errorf("invalid age %q", s)
			}
			return time.Duration(v * float64(d)), nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid age %q (use e.g. 30d, 2w, 12h)", s)
	}
	return d, nil
}

func (a *app) completeTrash(_ *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 || a.openVault(false) != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	items, _ := a.vault.Trash()
	var out []string
	for _, it := range items {
		p := strings.TrimPrefix(it.Path, vault.TrashDir+"/")
		if strings.HasPrefix(strings.ToLower(p), strings.ToLower(prefix)) {
			out = append(out, p)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
