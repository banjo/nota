package cli

import (
	"context"
	"fmt"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/protocol"
	nsync "github.com/banjo/nota/internal/sync"
)

// remoteClient returns a client for the vault's remote with its token.
func (a *app) remoteClient() (*nsync.Client, nsync.Remote, error) {
	r, err := a.loadRemote()
	if err != nil {
		return nil, r, err
	}
	tok, err := nsync.LoadToken(r.Account(), r.TokenStore)
	if err != nil {
		return nil, r, err
	}
	return &nsync.Client{BaseURL: r.URL, Vault: r.Vault, Token: tok, UserAgent: "nota/" + Version}, r, nil
}

// historyPath resolves a note for the history commands: a note in the
// vault (path, title or prefix), else the path as given, so deleted
// notes have a history too.
func (a *app) historyPath(ref string) (string, error) {
	p, err := a.resolve(ref)
	if err == nil {
		return p, nil
	}
	if c, cerr := protocol.CleanPath(ref); cerr == nil {
		return c, nil
	}
	return "", err
}

func (a *app) historyCmd() *cobra.Command {
	var version int64
	cmd := &cobra.Command{
		Use:   "history <note>",
		Short: "List the versions of a note on the sync server",
		Long: `List every version the sync server has of a note, newest first,
through renames. --version N prints that version's content. Restore one
with "nota restore <note> <version>".`,
		Args:              exactArgs(1, "a note"),
		ValidArgsFunction: a.completeNotes,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := a.remoteClient()
			if err != nil {
				return err
			}
			p, err := a.historyPath(args[0])
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			if version > 0 {
				e, content, err := c.HistoryVersion(ctx, p, version)
				if err != nil {
					return historyErr(err)
				}
				if a.json {
					return a.printJSON(map[string]any{"entry": e, "content": content})
				}
				if content == nil {
					return &Error{Code: ExitNotFound, Kind: "deleted", Message: fmt.Sprintf("version %d of %s is a delete", version, e.Path)}
				}
				_, err = fmt.Fprint(a.stdout, *content)
				return err
			}
			h, err := c.History(ctx, p)
			if err != nil {
				return historyErr(err)
			}
			if a.json {
				return a.printJSON(h)
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "VERSION\tMODIFIED\tDEVICE\tSIZE\tCHANGE")
			for i, e := range h.Versions {
				// A move is a tombstone of the old path plus the new
				// path's version; the tombstone says nothing new here.
				if e.Deleted && i > 0 && h.Versions[i-1].MovedFrom == e.Path && h.Versions[i-1].Version == e.Version+1 {
					continue
				}
				change := ""
				switch {
				case e.Deleted:
					change = "deleted"
				case e.MovedFrom != "":
					change = "moved from " + e.MovedFrom
				}
				if e.Path != h.Path && change == "" {
					change = "as " + e.Path
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%s\n", e.Version, e.ModifiedAt.Local().Format(time.RFC3339), e.DeviceID, e.Size, change)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().Int64Var(&version, "version", 0, "print the content of this version")
	return cmd
}

func historyErr(err error) error {
	if nsync.IsCode(err, protocol.CodeNotFound) {
		ae := err.(*nsync.APIError)
		return &Error{Code: ExitNotFound, Kind: "not_found", Message: "server: " + ae.Body.Message}
	}
	return syncErr(err)
}

func (a *app) restoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <note> <version>",
		Short: "Restore an old version of a note (as a new version)",
		Long: `Make an old version of a note its newest version on the sync server,
then sync so the file here has it. History is never rewritten: the
restore is a new version, and the one it replaces stays in the history.
See the versions with "nota history <note>".`,
		Args:              exactArgs(2, "a note and a version"),
		ValidArgsFunction: a.completeNotes,
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || version <= 0 {
				return usageErr("bad version %q", args[1])
			}
			c, r, err := a.remoteClient()
			if err != nil {
				return err
			}
			p, err := a.historyPath(args[0])
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()
			e, err := c.Restore(ctx, p, version)
			if err != nil {
				return historyErr(err)
			}
			a.attrs = append(a.attrs, "restored", version, "version", e.Version)
			out := map[string]any{"restored": version, "entry": e, "synced": false}
			// Bring the file here up to date; a running sync daemon does
			// it on its own.
			eng, _, err := a.engine()
			var res *nsync.Result
			if err == nil {
				res, err = eng.Sync(ctx)
				eng.Close()
			}
			if err == nil {
				out["synced"] = true
				out["sync"] = res
			}
			if a.json {
				return a.printJSON(out)
			}
			fmt.Fprintf(a.stdout, "restored %s to version %d (now version %d)\n", p, version, e.Version)
			if err != nil {
				fmt.Fprintf(a.stderr, "nota: not synced yet (%v); it arrives with the next nota sync\n", err)
				return nil
			}
			a.printResult(res, r, false)
			return nil
		},
	}
	return cmd
}
