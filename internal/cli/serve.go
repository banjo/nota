package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/server"
)

// Server environment variables. Flags win over them.
const (
	EnvServerAddr        = "NOTA_SERVER_ADDR"
	EnvServerData        = "NOTA_SERVER_DATA"
	EnvServerMaxFileSize = "NOTA_SERVER_MAX_FILE_SIZE"
	EnvServerLogFormat   = "NOTA_SERVER_LOG_FORMAT"
	// EnvServerPublicURL is the API's own public URL.
	EnvServerPublicURL = "NOTA_SERVER_PUBLIC_URL"
	// EnvServerWebOrigin lists the web app's origins, comma-separated;
	// the first is where sign-in links point.
	EnvServerWebOrigin = "NOTA_SERVER_WEB_ORIGIN"
	// EnvServerRPID is the WebAuthn relying party id (passkeys are off
	// without it).
	EnvServerRPID = "NOTA_SERVER_RP_ID"
	// EnvServerCookieDomain is the session cookie's Domain.
	EnvServerCookieDomain = "NOTA_SERVER_COOKIE_DOMAIN"
	// EnvServerCookieInsecure=1 drops the cookie's Secure flag (local http only).
	EnvServerCookieInsecure = "NOTA_SERVER_COOKIE_INSECURE"
	// EnvServerSignup is invite (default), open or closed.
	EnvServerSignup = "NOTA_SERVER_SIGNUP"
	// EnvServerTrustedProxies lists proxies (IPs, CIDRs or "private")
	// whose X-Forwarded-For is believed.
	EnvServerTrustedProxies = "NOTA_SERVER_TRUSTED_PROXIES"
	// EnvServerPollInterval is the device-code poll interval (default 5s).
	EnvServerPollInterval = "NOTA_SERVER_POLL_INTERVAL"
	// EnvServerHooks=on runs each vault's .nota/hooks.toml on the server
	// (default off).
	EnvServerHooks = "NOTA_SERVER_HOOKS"
)

type serveFlags struct {
	addr, data, maxSize, logFormat, addrFile string
}

// envOr returns the environment variable or def.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// defaultDataDir is ~/.local/share/nota/server (or $XDG_DATA_HOME/nota/server).
func defaultDataDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "nota", "server")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "nota", "server")
	}
	return "nota-data"
}

// parseSize reads a byte size: 1048576, 100MB, 1.5GiB, 512k.
func parseSize(s string) (int64, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30}, {"KB", 1e3}, {"MB", 1e6}, {"GB", 1e9},
		{"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30}, {"B", 1}} {
		if strings.HasSuffix(t, u.suffix) {
			t, mult = strings.TrimSpace(strings.TrimSuffix(t, u.suffix)), u.mult
			break
		}
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || f <= 0 {
		return 0, fmt.Errorf("bad size %q (want e.g. 104857600, 100MB or 1GiB)", s)
	}
	return int64(f * float64(mult)), nil
}

func (a *app) serveCmd() *cobra.Command {
	f := &serveFlags{}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the sync server",
		Long: `Run the Nota sync server: vaults with version history for several
users, synced by "nota sync" over HTTP (protocol in docs/sync-protocol.md).

Everything the server keeps is in the data directory (--data): the
SQLite database nota.db and the blob store blobs/. Settings can come
from the environment instead of flags: $NOTA_SERVER_ADDR,
$NOTA_SERVER_DATA, $NOTA_SERVER_MAX_FILE_SIZE, $NOTA_SERVER_LOG_FORMAT.

Accounts (email + passkey) are configured only through the environment:
$NOTA_SERVER_PUBLIC_URL (this API), $NOTA_SERVER_WEB_ORIGIN (the web
app's origins, comma-separated), $NOTA_SERVER_RP_ID (the passkey domain;
passkeys are off without it), $NOTA_SERVER_COOKIE_DOMAIN,
$NOTA_SERVER_SIGNUP (invite, open or closed; default invite) and
$NOTA_SERVER_TRUSTED_PROXIES (proxies whose X-Forwarded-For is trusted,
e.g. "private"). See docs/deploy.md.

The server keeps a working copy and an index of every vault in
<data>/vaults/ (for the web API and HTTP MCP). With $NOTA_SERVER_HOOKS=on
it also runs each vault's .nota/hooks.toml on changes from clients (off
by default: hooks are shell commands from the synced vault).

Bootstrap a user and a device token with

  nota serve user add you@example.com
  nota serve token create you@example.com --device laptop

and give the token to "nota remote add". "user add" also prints a setup
code to register the account's first passkey in the web app. The server
logs one line per request to stderr and shuts down gracefully on SIGINT
or SIGTERM.`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error { return a.runServe(cmd, f) },
	}
	pf := cmd.PersistentFlags()
	pf.StringVar(&f.data, "data", "", "data directory (default $NOTA_SERVER_DATA or ~/.local/share/nota/server)")
	fl := cmd.Flags()
	fl.StringVar(&f.addr, "addr", "", "listen address (default $NOTA_SERVER_ADDR or :8080)")
	fl.StringVar(&f.maxSize, "max-file-size", "", "largest file accepted, e.g. 100MB (default $NOTA_SERVER_MAX_FILE_SIZE or 100MiB)")
	fl.StringVar(&f.logFormat, "log-format", "", "log format: text or json (default $NOTA_SERVER_LOG_FORMAT or text)")
	fl.StringVar(&f.addrFile, "addr-file", "", "write the bound address to this file once listening (for scripts)")
	cmd.AddCommand(a.serveUserCmd(f), a.serveTokenCmd(f), a.serveInviteCmd(f), a.serveAuthCmd(f), a.serveRebuildCmd(f), a.serveVaultCmd(f))
	return cmd
}

// setupTTL is how long the setup code of `nota serve user add` lasts.
const setupTTL = 30 * 24 * time.Hour

func (a *app) serveInviteCmd(f *serveFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "invite", Short: "Manage sign-up invites",
		Long: `With the default sign-up mode (NOTA_SERVER_SIGNUP=invite) people need an
invite code to create an account. An invite works once; with --email only
that address can use it.`}
	var email, note string
	var ttl time.Duration
	create := &cobra.Command{
		Use:   "create [--email addr]",
		Short: "Create an invite code (printed once)",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				code, inv, err := st.CreateInvite(ctx, email, note, ttl)
				if err != nil {
					return usageErr("%v", err)
				}
				if a.json {
					return a.printJSON(map[string]any{"invite": inv, "code": code, "link": signupLink(code)})
				}
				fmt.Fprintln(a.stdout, code)
				if l := signupLink(code); l != "" {
					fmt.Fprintln(a.stdout, l)
				}
				fmt.Fprintf(a.stderr, "invite %d, valid until %s; it is shown only once\n", inv.ID, inv.ExpiresAt.Format(time.RFC3339))
				return nil
			})
		},
	}
	create.Flags().StringVar(&email, "email", "", "only this email can use the invite")
	create.Flags().StringVar(&note, "note", "", "a note for the invite list")
	create.Flags().DurationVar(&ttl, "expires", server.DefaultInviteTTL, "how long the invite is valid")
	list := &cobra.Command{
		Use:   "list",
		Short: "List invites",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				invs, err := st.Invites(ctx)
				if err != nil {
					return err
				}
				if a.json {
					return a.printJSON(invs)
				}
				tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "ID	EMAIL	NOTE	EXPIRES	STATUS")
				for _, inv := range invs {
					status := "open"
					if inv.UsedAt != nil {
						status = "used " + inv.UsedAt.Format(time.RFC3339)
					}
					fmt.Fprintf(tw, "%d	%s	%s	%s	%s\n", inv.ID, dash(inv.Email), dash(inv.Note), inv.ExpiresAt.Format(time.RFC3339), status)
				}
				return tw.Flush()
			})
		},
	}
	revoke := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Delete an unused invite",
		Args:  exactArgs(1, "an invite id (see nota serve invite list)"),
		RunE: func(_ *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageErr("bad invite id %q", args[0])
			}
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				if err := st.RevokeInvite(ctx, id); err != nil {
					return serverErr(err)
				}
				if a.json {
					return a.printJSON(map[string]any{"revoked": id})
				}
				fmt.Fprintf(a.stdout, "revoked invite %d\n", id)
				return nil
			})
		},
	}
	cmd.AddCommand(create, list, revoke)
	return cmd
}

func (a *app) serveAuthCmd(f *serveFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Approve device sign-ins from the server side",
		Long: `Pending sign-ins come from "nota login" on a device. Users approve them
in the web app; an admin can approve one here instead, for example before
the web app exists or for a user without a passkey yet.`}
	pending := &cobra.Command{
		Use:   "pending",
		Short: "List pending device sign-ins",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				ds, err := st.PendingDeviceAuths(ctx)
				if err != nil {
					return err
				}
				if a.json {
					return a.printJSON(ds)
				}
				tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "ID\tDEVICE\tSTARTED\tEXPIRES")
				for _, d := range ds {
					fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", d.ID, d.Device, d.CreatedAt.Format(time.RFC3339), d.ExpiresAt.Format(time.RFC3339))
				}
				return tw.Flush()
			})
		},
	}
	var id int64
	var device string
	var wait time.Duration
	var deny bool
	approve := &cobra.Command{
		Use:   "approve <email> (--id N | --device NAME) [--wait 30s]",
		Short: "Approve (or --deny) a pending sign-in for a user",
		Long: `Approve a pending "nota login" for the user with that email: the one
with --id (see "nota serve auth pending"), or the newest one from the
device named --device. --wait keeps looking for that long.`,
		Args: exactArgs(1, "an email address"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (id == 0) == (device == "") {
				return usageErr("give --id or --device")
			}
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				u, err := st.UserByEmail(ctx, args[0])
				if err != nil {
					return serverErr(err)
				}
				deadline := time.Now().Add(wait)
				for {
					ds, err := st.PendingDeviceAuths(ctx)
					if err != nil {
						return err
					}
					for _, d := range ds {
						if (id != 0 && d.ID == id) || (id == 0 && d.Device == device) {
							if err := st.DecideDeviceAuth(ctx, d.ID, u.ID, !deny); err != nil {
								return serverErr(err)
							}
							verb := "approved"
							if deny {
								verb = "denied"
							}
							if a.json {
								return a.printJSON(map[string]any{verb: d, "user": u.Email})
							}
							fmt.Fprintf(a.stdout, "%s sign-in %d of %s for %s\n", verb, d.ID, d.Device, u.Email)
							return nil
						}
					}
					if !time.Now().Before(deadline) {
						return &Error{Code: ExitNotFound, Kind: "not_found", Message: "no matching pending sign-in"}
					}
					select {
					case <-cmd.Context().Done():
						return cmd.Context().Err()
					case <-time.After(200 * time.Millisecond):
					}
				}
			})
		},
	}
	fl := approve.Flags()
	fl.Int64Var(&id, "id", 0, "the pending sign-in's id")
	fl.StringVar(&device, "device", "", "the newest pending sign-in from this device name")
	fl.DurationVar(&wait, "wait", 0, "keep looking for a matching sign-in this long")
	fl.BoolVar(&deny, "deny", false, "deny instead of approving")
	cmd.AddCommand(pending, approve)
	return cmd
}

func (a *app) serveRebuildCmd(f *serveFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "rebuild",
		Short: "Delete the server's working copies and indexes (rebuilt from the log on next use)",
		Long: `The server keeps a working copy of every vault, with an index, in
<data>/vaults/. They are only a cache of the version log: this deletes
them, and the server rebuilds each one the next time the vault is used.
Stop the server first.`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			dir, err := f.dataDir()
			if err != nil {
				return err
			}
			if err := server.RebuildWorkspaces(dir); err != nil {
				return err
			}
			if a.json {
				return a.printJSON(map[string]any{"rebuilt": dir})
			}
			fmt.Fprintf(a.stdout, "deleted the working copies in %s; they are rebuilt on next use\n", dir)
			return nil
		},
	}
}

func (a *app) serveVaultCmd(f *serveFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "vault", Short: "List or delete users' vaults"}
	list := &cobra.Command{
		Use:   "list <email>",
		Short: "List a user's vaults",
		Args:  exactArgs(1, "an email address"),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				u, err := st.UserByEmail(ctx, args[0])
				if err != nil {
					return serverErr(err)
				}
				vs, err := st.Vaults(ctx, u.ID)
				if err != nil {
					return err
				}
				if a.json {
					return a.printJSON(vs)
				}
				for _, v := range vs {
					fmt.Fprintf(a.stdout, "%s\tcursor %d\t%s\n", v.Name, v.Cursor, v.CreatedAt.Format(time.RFC3339))
				}
				return nil
			})
		},
	}
	var yes bool
	rm := &cobra.Command{
		Use:   "rm <email> <vault> --yes",
		Short: "Delete a vault with its whole history from the server",
		Long: `Delete a user's vault from the server: its files, every version and
its working copy. Devices keep their local files; their next sync fails
until they connect to another vault. This cannot be undone.`,
		Args: exactArgs(2, "an email address and a vault name"),
		RunE: func(_ *cobra.Command, args []string) error {
			if !yes {
				return usageErr("this deletes the vault and its history; pass --yes")
			}
			dir, err := f.dataDir()
			if err != nil {
				return err
			}
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				u, err := st.UserByEmail(ctx, args[0])
				if err != nil {
					return serverErr(err)
				}
				id, err := st.VaultID(ctx, u.ID, args[1])
				if err != nil {
					return serverErr(err)
				}
				if err := st.DeleteVault(ctx, u.ID, args[1]); err != nil {
					return serverErr(err)
				}
				_ = os.RemoveAll(filepath.Join(dir, "vaults", strconv.FormatInt(id, 10)))
				if a.json {
					return a.printJSON(map[string]any{"deleted": args[1], "user": u.Email})
				}
				fmt.Fprintf(a.stdout, "deleted vault %s of %s\n", args[1], u.Email)
				return nil
			})
		},
	}
	rm.Flags().BoolVar(&yes, "yes", false, "really delete")
	cmd.AddCommand(list, rm)
	return cmd
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (f *serveFlags) dataDir() (string, error) {
	d := f.data
	if d == "" {
		d = envOr(EnvServerData, defaultDataDir())
	}
	d, err := expandTilde(d)
	if err != nil {
		return "", err
	}
	return filepath.Abs(d)
}

func expandTilde(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, p[1:]), nil
}

func (a *app) runServe(cmd *cobra.Command, f *serveFlags) error {
	dir, err := f.dataDir()
	if err != nil {
		return err
	}
	addr := f.addr
	if addr == "" {
		addr = envOr(EnvServerAddr, ":8080")
	}
	maxSize := int64(server.DefaultMaxFileSize)
	if s := f.maxSize; s != "" || os.Getenv(EnvServerMaxFileSize) != "" {
		if s == "" {
			s = os.Getenv(EnvServerMaxFileSize)
		}
		if maxSize, err = parseSize(s); err != nil {
			return usageErr("--max-file-size: %v", err)
		}
	}
	cfg, err := serverConfigFromEnv()
	if err != nil {
		return usageErr("%v", err)
	}
	cfg.DataDir, cfg.MaxFileSize = dir, maxSize
	format := f.logFormat
	if format == "" {
		format = envOr(EnvServerLogFormat, "text")
	}
	var h slog.Handler
	switch format {
	case "text":
		h = slog.NewTextHandler(a.stderr, nil)
	case "json":
		h = slog.NewJSONHandler(a.stderr, nil)
	default:
		return usageErr("--log-format: want text or json, got %q", format)
	}
	log := slog.New(h)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg.Logger, cfg.Version = log, Version
	srv, err := server.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer srv.Close()
	err = srv.ListenAndServe(ctx, addr, 10*time.Second, func(ln net.Addr) {
		log.Info("nota serve: listening", "addr", ln.String(), "data", dir, "max_file_size", maxSize, "version", Version,
			"public_url", cfg.PublicURL, "web_origins", strings.Join(cfg.WebOrigins, ","), "rp_id", cfg.RPID,
			"signup", string(cfg.Signup), "trusted_proxies", len(cfg.TrustedProxies), "hooks", cfg.Hooks)
		if f.addrFile != "" {
			if err := os.WriteFile(f.addrFile, []byte(ln.String()+"\n"), 0o644); err != nil {
				log.Error("write --addr-file", "err", err)
			}
		}
	})
	if err != nil {
		return err
	}
	log.Info("nota serve: stopped")
	return nil
}

// serverConfigFromEnv reads the account and web settings of the server
// from the environment.
func serverConfigFromEnv() (server.Config, error) {
	var cfg server.Config
	cfg.PublicURL = strings.TrimRight(os.Getenv(EnvServerPublicURL), "/")
	cfg.WebOrigins = webOrigins()
	cfg.RPID = strings.TrimSpace(os.Getenv(EnvServerRPID))
	cfg.CookieDomain = strings.TrimSpace(os.Getenv(EnvServerCookieDomain))
	cfg.CookieInsecure = truthy(os.Getenv(EnvServerCookieInsecure))
	var err error
	if cfg.Signup, err = server.ParseSignupMode(os.Getenv(EnvServerSignup)); err != nil {
		return cfg, err
	}
	if cfg.TrustedProxies, err = server.ParseProxies(os.Getenv(EnvServerTrustedProxies)); err != nil {
		return cfg, err
	}
	switch h := strings.ToLower(strings.TrimSpace(os.Getenv(EnvServerHooks))); h {
	case "", "off", "0", "false":
	case "on", "1", "true":
		cfg.Hooks = true
	default:
		return cfg, fmt.Errorf("$%s: want on or off, got %q", EnvServerHooks, h)
	}
	if v := os.Getenv(EnvServerPollInterval); v != "" {
		if cfg.PollInterval, err = time.ParseDuration(v); err != nil {
			return cfg, fmt.Errorf("$%s: %w", EnvServerPollInterval, err)
		}
	}
	return cfg, nil
}

// webOrigins reads $NOTA_SERVER_WEB_ORIGIN as a list.
func webOrigins() []string {
	var out []string
	for _, o := range strings.Split(os.Getenv(EnvServerWebOrigin), ",") {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			out = append(out, o)
		}
	}
	return out
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// signupLink is where someone with an invite creates their account.
func signupLink(code string) string {
	o := webOrigins()
	if len(o) == 0 {
		return ""
	}
	return o[0] + "/app/signup?invite=" + code
}

// withStore opens the data directory for an admin command.
func (a *app) withStore(f *serveFlags, fn func(context.Context, *server.Store) error) error {
	dir, err := f.dataDir()
	if err != nil {
		return err
	}
	ctx := context.Background()
	st, err := server.OpenData(ctx, dir)
	if err != nil {
		return err
	}
	defer st.Close()
	return fn(ctx, st)
}

func serverErr(err error) error {
	switch {
	case errors.Is(err, server.ErrNotFound):
		return &Error{Code: ExitNotFound, Kind: "not_found", Message: err.Error()}
	case errors.Is(err, server.ErrExists):
		return &Error{Code: ExitConflict, Kind: "exists", Message: err.Error()}
	}
	return err
}

func (a *app) serveUserCmd(f *serveFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "Manage the server's users"}
	add := &cobra.Command{
		Use:   "add <email>",
		Short: "Create a user",
		Args:  exactArgs(1, "an email address"),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				email, err := server.NormalizeEmail(args[0])
				if err != nil {
					return usageErr("%v", err)
				}
				u, err := st.AddUser(ctx, email)
				if err != nil {
					return serverErr(err)
				}
				// The user has no passkey yet: an invite bound to the
				// email lets them register one in the web app.
				code, inv, err := st.CreateInvite(ctx, u.Email, "setup", setupTTL)
				if err != nil {
					return err
				}
				if a.json {
					return a.printJSON(map[string]any{"user": u, "setup_code": code, "setup_link": signupLink(code), "expires_at": inv.ExpiresAt})
				}
				fmt.Fprintf(a.stdout, "created user %s\n", u.Email)
				fmt.Fprintf(a.stdout, "setup code %s (valid until %s): register a passkey with it", code, inv.ExpiresAt.Format(time.RFC3339))
				if l := signupLink(code); l != "" {
					fmt.Fprintf(a.stdout, " at %s", l)
				}
				fmt.Fprintln(a.stdout)
				return nil
			})
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List users",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				us, err := st.Users(ctx)
				if err != nil {
					return err
				}
				if a.json {
					if us == nil {
						us = []server.User{}
					}
					return a.printJSON(us)
				}
				for _, u := range us {
					fmt.Fprintf(a.stdout, "%s\t%s\n", u.Email, u.CreatedAt.Format(time.RFC3339))
				}
				return nil
			})
		},
	}
	var yes bool
	rm := &cobra.Command{
		Use:   "rm <email> --yes",
		Short: "Delete a user with everything the server keeps for it",
		Long: `Delete a user and everything owned by it: device tokens, the web app's
sessions, passkeys, recovery codes, invites bound to the address, every
vault with its files, version log and working copy, and the blob files no
other user references (the bytes are content addressed and shared).
Devices keep their local notes; their tokens stop working. Without --yes
it only prints what it would remove. This cannot be undone.`,
		Args: exactArgs(1, "an email address"),
		RunE: func(_ *cobra.Command, args []string) error {
			dir, err := f.dataDir()
			if err != nil {
				return err
			}
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				rmv, err := st.PlanUserRemoval(ctx, args[0])
				if err != nil {
					return serverErr(err)
				}
				if !yes {
					a.printRemoval(rmv, false)
					return usageErr("this deletes the user and everything owned by it; pass --yes")
				}
				if rmv, err = st.DeleteUser(ctx, args[0]); err != nil {
					return serverErr(err)
				}
				files, bytes, err := server.RemoveUserData(ctx, st, dir, rmv)
				rmv.BlobsReclaimed, rmv.BytesReclaimed = files, bytes
				a.printRemoval(rmv, true)
				return err
			})
		},
	}
	rm.Flags().BoolVar(&yes, "yes", false, "really delete")
	cmd.AddCommand(add, list, rm)
	return cmd
}

// printRemoval shows what deleting a user removes (deleted=false: what it
// would remove).
func (a *app) printRemoval(rm server.UserRemoval, deleted bool) {
	if a.json {
		_ = a.printJSON(rm)
		return
	}
	verb := "would delete"
	if deleted {
		verb = "deleted"
	}
	fmt.Fprintf(a.stdout, "%s user %s (created %s)\n", verb, rm.User.Email, rm.User.CreatedAt.Format(time.RFC3339))
	for _, v := range rm.Vaults {
		fmt.Fprintf(a.stdout, "  vault %s (id %d, cursor %d)\n", v.Name, v.ID, v.Cursor)
	}
	fmt.Fprintf(a.stdout, "  %d token(s), %d session(s), %d passkey(s), %d recovery code(s), %d invite(s)\n",
		rm.Tokens, rm.Sessions, rm.Passkeys, rm.RecoveryCodes, rm.Invites)
	fmt.Fprintf(a.stdout, "  %d blob(s), %d bytes referenced; %d blob file(s), %d bytes reclaimed from disk\n",
		rm.Blobs, rm.BlobBytes, rm.BlobsReclaimed, rm.BytesReclaimed)
}

func (a *app) serveTokenCmd(f *serveFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "token", Short: "Manage device tokens"}
	var device string
	create := &cobra.Command{
		Use:   "create <email> --device <name>",
		Short: "Create a device token (printed once)",
		Args:  exactArgs(1, "an email address"),
		RunE: func(_ *cobra.Command, args []string) error {
			if strings.TrimSpace(device) == "" {
				return usageErr("--device is required")
			}
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				u, err := st.UserByEmail(ctx, args[0])
				if err != nil {
					return serverErr(err)
				}
				secret, t, err := st.CreateToken(ctx, u.ID, device)
				if err != nil {
					return err
				}
				t.Email = u.Email
				if a.json {
					return a.printJSON(struct {
						server.Token
						Secret string `json:"token"`
					}{t, secret})
				}
				fmt.Fprintln(a.stdout, secret)
				fmt.Fprintf(a.stderr, "token %d for %s on %s; it is shown only once\n", t.ID, u.Email, t.Device)
				return nil
			})
		},
	}
	create.Flags().StringVar(&device, "device", "", "device name, shown in conflict copies and the token list")
	list := &cobra.Command{
		Use:   "list [email]",
		Short: "List device tokens",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				var uid int64
				if len(args) == 1 {
					u, err := st.UserByEmail(ctx, args[0])
					if err != nil {
						return serverErr(err)
					}
					uid = u.ID
				}
				ts, err := st.Tokens(ctx, uid)
				if err != nil {
					return err
				}
				if a.json {
					if ts == nil {
						ts = []server.Token{}
					}
					return a.printJSON(ts)
				}
				tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "ID\tUSER\tDEVICE\tCREATED\tLAST USED\tSTATUS")
				for _, t := range ts {
					used, status := "never", "active"
					if t.LastUsedAt != nil {
						used = t.LastUsedAt.Format(time.RFC3339)
					}
					if t.RevokedAt != nil {
						status = "revoked " + t.RevokedAt.Format(time.RFC3339)
					}
					fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", t.ID, t.Email, t.Device, t.CreatedAt.Format(time.RFC3339), used, status)
				}
				return tw.Flush()
			})
		},
	}
	revoke := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke a device token",
		Args:  exactArgs(1, "a token id (see nota serve token list)"),
		RunE: func(_ *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageErr("bad token id %q", args[0])
			}
			return a.withStore(f, func(ctx context.Context, st *server.Store) error {
				if err := st.RevokeToken(ctx, id); err != nil {
					return serverErr(err)
				}
				if a.json {
					return a.printJSON(map[string]any{"revoked": id})
				}
				fmt.Fprintf(a.stdout, "revoked token %d\n", id)
				return nil
			})
		},
	}
	cmd.AddCommand(create, list, revoke)
	return cmd
}
