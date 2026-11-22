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

Bootstrap a user and a device token with

  nota serve user add you@example.com
  nota serve token create you@example.com --device laptop

and give the token to "nota remote add". The server logs one line per
request to stderr and shuts down gracefully on SIGINT or SIGTERM.`,
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
	cmd.AddCommand(a.serveUserCmd(f), a.serveTokenCmd(f))
	return cmd
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
	srv, err := server.New(ctx, server.Config{DataDir: dir, MaxFileSize: maxSize, Logger: log})
	if err != nil {
		return err
	}
	defer srv.Close()
	err = srv.ListenAndServe(ctx, addr, 10*time.Second, func(ln net.Addr) {
		log.Info("nota serve: listening", "addr", ln.String(), "data", dir, "max_file_size", maxSize, "version", Version)
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
				if a.json {
					return a.printJSON(u)
				}
				fmt.Fprintf(a.stdout, "created user %s\n", u.Email)
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
	cmd.AddCommand(add, list)
	return cmd
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
