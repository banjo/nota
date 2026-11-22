package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/protocol"
	nsync "github.com/banjo/nota/internal/sync"
)

func (a *app) remoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Configure the sync server of the vault",
		Long: `A vault syncs with one vault on a Nota server (nota serve). The
remote lives in .nota/sync.toml; the device token is kept in the OS
keyring, or in ~/.config/nota/credentials (mode 0600) where there is no
keyring, such as a headless Linux box. Set NOTA_KEYRING=file to always
use the file, and NOTA_TOKEN to pass a token without storing it.`,
	}
	cmd.AddCommand(a.remoteAddCmd(), a.remoteShowCmd(), a.remoteRmCmd())
	return cmd
}

func defaultDevice() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "device"
	}
	return strings.Split(h, ".")[0]
}

func (a *app) remoteAddCmd() *cobra.Command {
	var token, vaultName, device string
	cmd := &cobra.Command{
		Use:   "add <url> [--token <token>] [--remote-vault name]",
		Short: "Connect the vault to a sync server",
		Long: `Connect the vault to a vault on a sync server. The server vault is
created when it does not exist yet. The token is the one "nota login"
stored for this server and device, or --token (from the server admin:
nota serve token create <email> --device <name>).`,
		Args: exactArgs(1, "the server URL"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.openVault(false); err != nil {
				return err
			}
			u, err := nsync.NormalizeURL(args[0])
			if err != nil {
				return usageErr("%v", err)
			}
			if token == "" {
				token = os.Getenv(nsync.EnvToken)
			}
			if token == "" {
				// A token from `nota login` for this server and device.
				token, _, _ = nsync.FindToken(nsync.Remote{URL: u, Device: device}.Account())
			}
			if token == "" {
				return usageErr("no token: sign in with `nota login %s` or pass --token", u)
			}
			if !protocol.ValidVaultName(vaultName) {
				return usageErr("bad --remote-vault %q: use 1-64 letters, digits, '.', '_' or '-'", vaultName)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			c := &nsync.Client{BaseURL: u, Vault: vaultName, Token: token, UserAgent: "nota/" + Version}
			vs, err := c.Vaults(ctx)
			if err != nil {
				return syncErr(err)
			}
			created := true
			for _, v := range vs {
				if v.Name == vaultName {
					created = false
				}
			}
			if created {
				if _, err := c.CreateVault(ctx, vaultName); err != nil {
					return syncErr(err)
				}
			}
			r := nsync.Remote{URL: u, Vault: vaultName, Device: device}
			store, why, err := nsync.SaveToken(r.Account(), token)
			if err != nil {
				return err
			}
			r.TokenStore = store
			if err := nsync.SaveRemote(a.vault.MetaDir(), r); err != nil {
				return err
			}
			if store == nsync.StoreFile {
				p, _ := nsync.CredentialsPath()
				fmt.Fprintf(a.stderr, "nota: no OS keyring (%s); the token is stored in %s (mode 0600)\n", why, p)
			}
			if a.json {
				return a.printJSON(map[string]any{"remote": r, "created": created})
			}
			verb := "using"
			if created {
				verb = "created"
			}
			fmt.Fprintf(a.stdout, "remote %s, vault %s (%s), device %s\n", u, vaultName, verb, device)
			return nil
		},
	}
	fl := cmd.Flags()
	fl.StringVar(&token, "token", "", "device token (or $NOTA_TOKEN)")
	// --vault is already the global vault directory flag, so the name on
	// the server has its own flag.
	fl.StringVar(&vaultName, "remote-vault", "main", "vault name on the server (created if missing)")
	fl.StringVar(&device, "device", defaultDevice(), "this machine's name, used in conflict copies")
	return cmd
}

func (a *app) loadRemote() (nsync.Remote, error) {
	if err := a.openVault(false); err != nil {
		return nsync.Remote{}, err
	}
	r, ok, err := nsync.LoadRemote(a.vault.MetaDir())
	if err != nil {
		return r, err
	}
	if !ok {
		return r, &Error{Code: ExitNotFound, Kind: "no_remote",
			Message: "the vault has no sync remote; add one with `nota remote add <url> --token <token>`"}
	}
	return r, nil
}

func (a *app) remoteShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the vault's sync remote",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			r, err := a.loadRemote()
			if err != nil {
				return err
			}
			_, terr := nsync.LoadToken(r.Account(), r.TokenStore)
			if a.json {
				return a.printJSON(map[string]any{"remote": r, "token": terr == nil})
			}
			tok := "stored in the " + r.TokenStore
			if terr != nil {
				tok = "missing: " + terr.Error()
			}
			fmt.Fprintf(a.stdout, "url     %s\nvault   %s\ndevice  %s\ntoken   %s\n", r.URL, r.Vault, r.Device, tok)
			return nil
		},
	}
}

func (a *app) remoteRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm",
		Short: "Disconnect the vault from its sync server (files stay as they are)",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			r, err := a.loadRemote()
			if err != nil {
				return err
			}
			if err := nsync.DeleteToken(r.Account(), r.TokenStore); err != nil {
				a.log.Warn("could not delete the stored token", "err", err)
			}
			if err := nsync.RemoveRemote(a.vault.MetaDir()); err != nil {
				return err
			}
			if a.json {
				return a.printJSON(map[string]any{"removed": r})
			}
			fmt.Fprintf(a.stdout, "removed remote %s (vault %s)\n", r.URL, r.Vault)
			return nil
		},
	}
}

// syncErr maps sync and server errors onto CLI errors.
func syncErr(err error) error {
	var ae *nsync.APIError
	var le *nsync.LockedError
	switch {
	case errors.As(err, &le):
		return &Error{Code: ExitError, Kind: "locked", Message: le.Error()}
	case errors.As(err, &ae):
		return &Error{Code: ExitError, Kind: ae.Code(), Message: "server: " + ae.Body.Message}
	}
	return err
}

// engine opens the sync engine of the vault.
func (a *app) engine() (*nsync.Engine, nsync.Remote, error) {
	r, err := a.loadRemote()
	if err != nil {
		return nil, r, err
	}
	tok, err := nsync.LoadToken(r.Account(), r.TokenStore)
	if err != nil {
		return nil, r, err
	}
	c := &nsync.Client{BaseURL: r.URL, Vault: r.Vault, Token: tok, UserAgent: "nota/" + Version}
	now, err := clock()
	if err != nil {
		return nil, r, err
	}
	e, err := nsync.Open(a.vault, c, nsync.Options{Device: r.Device, Now: now, Logger: a.log})
	if err != nil {
		return nil, r, syncErr(err)
	}
	return e, r, nil
}

func (a *app) syncCmd() *cobra.Command {
	var watch bool
	var interval, debounce time.Duration
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync the vault with its server",
		Long: `Run one sync pass: push local changes, merge what changed on both
sides, pull the server's changes. Edits to different lines of a note
merge; edits to the same lines (or to an attachment on both sides) keep
both versions, the local one in "Name (conflict DATE TIME device).md".
Remote deletes move files to .trash/.

With --watch it keeps running: it syncs after local changes, when the
server announces a change and every --interval, until interrupted.`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			e, r, err := a.engine()
			if err != nil {
				return err
			}
			defer e.Close()
			if watch {
				return a.watch(cmd, e, r, interval, debounce)
			}
			res, err := e.Sync(cmd.Context())
			if err != nil {
				return syncErr(err)
			}
			a.attrs = append(a.attrs, "sync_cursor", res.Cursor, "sync_changed", res.Changed())
			if a.json {
				if err := a.printJSON(res); err != nil {
					return err
				}
			} else {
				a.printResult(res, r, true)
			}
			if len(res.Errors) > 0 {
				return &Error{Code: ExitError, Kind: "sync_errors", Message: fmt.Sprintf("%d file(s) could not be synced", len(res.Errors))}
			}
			return nil
		},
	}
	fl := cmd.Flags()
	fl.BoolVar(&watch, "watch", false, "keep syncing until interrupted")
	fl.DurationVar(&interval, "interval", time.Minute, "with --watch: sync at least this often")
	fl.DurationVar(&debounce, "debounce", 500*time.Millisecond, "with --watch: wait this long after a local change")
	cmd.AddCommand(a.syncStatusCmd())
	return cmd
}

func (a *app) watch(cmd *cobra.Command, e *nsync.Engine, r nsync.Remote, interval, debounce time.Duration) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if !a.json {
		fmt.Fprintf(a.stderr, "nota: watching %s, syncing with %s (vault %s); Ctrl-C to stop\n", a.vault.Root(), r.URL, r.Vault)
	}
	err := e.Watch(ctx, nsync.WatchOptions{Poll: interval, Debounce: debounce, OnPass: func(res *nsync.Result, err error) {
		switch {
		case err != nil:
			fmt.Fprintf(a.stderr, "nota: sync failed (will retry): %v\n", err)
		case a.json:
			_ = a.printJSONLine(res)
		case res.Changed() || len(res.Errors) > 0:
			fmt.Fprintf(a.stdout, "%s\n", time.Now().Format("15:04:05"))
			a.printResult(res, r, false)
		}
	}})
	if !a.json {
		fmt.Fprintln(a.stderr, "nota: stopped watching")
	}
	return err
}

func (a *app) printResult(res *nsync.Result, r nsync.Remote, summary bool) {
	w := a.stdout
	line := func(kind, p string) { fmt.Fprintf(w, "  %-9s %s\n", kind, p) }
	for _, p := range res.Pushed {
		line("pushed", p)
	}
	for _, m := range res.Moved {
		line("moved", m.From+" -> "+m.To)
	}
	for _, p := range res.Deleted {
		line("deleted", p)
	}
	for _, p := range res.Merged {
		line("merged", p)
	}
	for _, c := range res.Conflicts {
		line("conflict", c.Path+" (your version: "+c.Copy+")")
	}
	for _, p := range res.Pulled {
		line("pulled", p)
	}
	for _, m := range res.Renamed {
		line("renamed", m.From+" -> "+m.To)
	}
	for _, p := range res.Removed {
		line("removed", p+" (moved to .trash/)")
	}
	for _, p := range res.Restored {
		line("restored", p+" (edited on one side, deleted on the other)")
	}
	for _, fe := range res.Errors {
		line("error", fe.Path+": "+fe.Message)
	}
	if !summary {
		return
	}
	if !res.Changed() && len(res.Errors) == 0 {
		fmt.Fprintf(w, "up to date with %s (vault %s, cursor %d)\n", r.URL, r.Vault, res.Cursor)
		return
	}
	var parts []string
	for _, c := range []struct {
		n    int
		word string
	}{{len(res.Pushed), "pushed"}, {len(res.Moved), "moved"}, {len(res.Deleted), "deleted"},
		{len(res.Merged), "merged"}, {len(res.Conflicts), "conflict(s)"}, {len(res.Pulled), "pulled"},
		{len(res.Renamed), "renamed"}, {len(res.Removed), "removed"}, {len(res.Restored), "restored"},
		{len(res.Errors), "error(s)"}} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.word))
		}
	}
	fmt.Fprintf(w, "synced with %s (vault %s): %s; cursor %d\n", r.URL, r.Vault, strings.Join(parts, ", "), res.Cursor)
}

func (a *app) syncStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the sync state: remote, cursor, unsynced local changes",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			r, err := a.loadRemote()
			if err != nil {
				return err
			}
			st, err := nsync.ReadStatus(a.vault, "")
			if err != nil {
				return err
			}
			// Ask the server how far ahead it is; offline is fine.
			server := map[string]any{"reachable": false}
			if tok, err := nsync.LoadToken(r.Account(), r.TokenStore); err == nil {
				ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
				c := &nsync.Client{BaseURL: r.URL, Vault: r.Vault, Token: tok, UserAgent: "nota/" + Version}
				vs, err := c.Vaults(ctx)
				cancel()
				if err != nil {
					server["error"] = err.Error()
				} else {
					server["reachable"] = true
					for _, v := range vs {
						if v.Name == r.Vault {
							server["cursor"] = v.Cursor
						}
					}
				}
			} else {
				server["error"] = err.Error()
			}
			if a.json {
				return a.printJSON(map[string]any{"remote": r, "status": st, "server": server})
			}
			w := a.stdout
			fmt.Fprintf(w, "remote   %s (vault %s, device %s)\n", r.URL, r.Vault, r.Device)
			last := "never"
			if !st.LastSync.IsZero() {
				last = st.LastSync.Local().Format(time.RFC3339)
			}
			fmt.Fprintf(w, "synced   %d files, cursor %d, last sync %s\n", st.Tracked, st.Cursor, last)
			if c, ok := server["cursor"].(int64); ok {
				behind := "up to date"
				if c > st.Cursor {
					behind = fmt.Sprintf("%d version(s) to pull", c-st.Cursor)
				}
				fmt.Fprintf(w, "server   cursor %d (%s)\n", c, behind)
			} else {
				fmt.Fprintf(w, "server   unreachable: %v\n", server["error"])
			}
			fmt.Fprintf(w, "local    %d changed, %d new, %d deleted (not pushed yet)\n", len(st.Changed), len(st.Added), len(st.Deleted))
			for _, p := range st.Changed {
				fmt.Fprintf(w, "  changed %s\n", p)
			}
			for _, p := range st.Added {
				fmt.Fprintf(w, "  new     %s\n", p)
			}
			for _, p := range st.Deleted {
				fmt.Fprintf(w, "  deleted %s\n", p)
			}
			if st.Running {
				fmt.Fprintf(w, "daemon   running (pid %d on %s)\n", st.Holder.PID, st.Holder.Host)
			} else {
				fmt.Fprintln(w, "daemon   not running")
			}
			return nil
		},
	}
}
