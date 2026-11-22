package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/protocol"
	nsync "github.com/banjo/nota/internal/sync"
)

// DefaultServer is the Nota server `nota login` uses when no URL is
// given and the vault has no remote.
const DefaultServer = "https://nota.app.banjoanton.com"

// EnvServerURL overrides DefaultServer.
const EnvServerURL = "NOTA_SERVER_URL"

// account is a server plus the device name a token is stored under.
type account struct {
	URL    string
	Device string
}

func (c account) key() string { return nsync.Remote{URL: c.URL, Device: c.Device}.Account() }

// resolveAccount picks the server and device for the account commands:
// the URL argument, else the vault's remote, else $NOTA_SERVER_URL,
// else DefaultServer; the --device flag, else the remote's device (for
// the same server), else the host name.
func (a *app) resolveAccount(arg, device string) (account, error) {
	var remote nsync.Remote
	var hasRemote bool
	if a.openVault(false) == nil {
		if r, ok, err := nsync.LoadRemote(a.vault.MetaDir()); err == nil && ok {
			remote, hasRemote = r, true
		}
	}
	u := arg
	switch {
	case u != "":
	case hasRemote:
		u = remote.URL
	default:
		u = envOr(EnvServerURL, DefaultServer)
	}
	u, err := nsync.NormalizeURL(u)
	if err != nil {
		return account{}, usageErr("%v", err)
	}
	if device == "" {
		if hasRemote && remote.URL == u && remote.Device != "" {
			device = remote.Device
		} else {
			device = defaultDevice()
		}
	}
	return account{URL: u, Device: device}, nil
}

func (a *app) loginCmd() *cobra.Command {
	var device string
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login [server-url]",
		Short: "Sign this device in to a Nota server",
		Long: `Sign this device in to a sync server with a device code: nota login
shows a short code and a link, you approve the sign-in in the browser
(with your passkey), and the device gets its own token, stored in the OS
keyring (or ~/.config/nota/credentials without one).

The server is the URL argument, else the vault's remote, else
$NOTA_SERVER_URL, else ` + DefaultServer + `. Afterwards connect a vault
with "nota remote add <url>" (no --token needed) and run "nota sync".
"nota devices" lists the signed-in devices; "nota logout" signs out.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			acc, err := a.resolveAccount(firstArg(args), device)
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			c := &nsync.Client{BaseURL: acc.URL, UserAgent: "nota/" + Version}
			da, err := c.StartDeviceAuth(ctx, acc.Device)
			if err != nil {
				return syncErr(err)
			}
			link := da.VerificationURIComplete
			if link == "" {
				link = da.VerificationURI
			}
			fmt.Fprintf(a.stderr, "To sign in %s to %s, open\n\n  %s\n\nand check that it shows the code %s. Waiting for approval (Ctrl-C to cancel)...\n",
				acc.Device, acc.URL, link, da.UserCode)
			if !noBrowser {
				openBrowser(link)
			}
			res, err := c.PollDeviceToken(ctx, da, nil)
			switch {
			case errors.Is(err, nsync.ErrDenied):
				return &Error{Code: ExitError, Kind: "denied", Message: "the sign-in was denied in the browser"}
			case errors.Is(err, nsync.ErrExpiredAuth):
				return &Error{Code: ExitError, Kind: "expired", Message: err.Error()}
			case errors.Is(err, context.Canceled):
				return &Error{Code: ExitError, Kind: "canceled", Message: "sign-in canceled"}
			case err != nil:
				return syncErr(err)
			}
			store, why, err := nsync.SaveToken(acc.key(), res.Token)
			if err != nil {
				return err
			}
			if store == nsync.StoreFile && why != nsync.EnvKeyring+"=file" {
				p, _ := nsync.CredentialsPath()
				fmt.Fprintf(a.stderr, "nota: no OS keyring (%s); the token is stored in %s (mode 0600)\n", why, p)
			}
			a.attrs = append(a.attrs, "server", acc.URL, "token_id", res.TokenID)
			if a.json {
				return a.printJSON(map[string]any{"server": acc.URL, "email": res.Email, "device": res.Device,
					"token_id": res.TokenID, "token_store": store})
			}
			fmt.Fprintf(a.stdout, "signed in to %s as %s (device %s)\n", acc.URL, res.Email, res.Device)
			if a.vault != nil {
				if _, ok, _ := nsync.LoadRemote(a.vault.MetaDir()); !ok {
					fmt.Fprintf(a.stdout, "connect this vault with: nota remote add %s\n", acc.URL)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&device, "device", "", "this machine's name (default: the vault remote's device or the host name)")
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "only print the link, do not open a browser")
	return cmd
}

func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

// openBrowser opens u in the default browser, best effort and in the
// background: the link is printed either way.
func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return
		}
		cmd = exec.Command("xdg-open", u)
	}
	if cmd.Start() == nil {
		go func() { _ = cmd.Wait() }()
	}
}

// accountClient returns a client with the stored token of an account.
func (a *app) accountClient(acc account) (*nsync.Client, error) {
	tok := os.Getenv(nsync.EnvToken)
	if tok == "" {
		var err error
		if tok, _, err = nsync.FindToken(acc.key()); err != nil {
			return nil, &Error{Code: ExitNotFound, Kind: "not_signed_in",
				Message: fmt.Sprintf("not signed in to %s as device %s; run `nota login %s`", acc.URL, acc.Device, acc.URL)}
		}
	}
	return &nsync.Client{BaseURL: acc.URL, Token: tok, UserAgent: "nota/" + Version}, nil
}

func (a *app) logoutCmd() *cobra.Command {
	var device string
	cmd := &cobra.Command{
		Use:   "logout [server-url]",
		Short: "Sign this device out: revoke its token and forget it",
		Long: `Revoke this device's token on the server and delete it from the
keyring or credentials file. Local vaults and their files stay as they
are; "nota sync" needs a new "nota login" afterwards.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			acc, err := a.resolveAccount(firstArg(args), device)
			if err != nil {
				return err
			}
			c, err := a.accountClient(acc)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			revoked := true
			if err := c.RevokeSelf(ctx); err != nil {
				if !nsync.IsCode(err, protocol.CodeUnauthorized) {
					return syncErr(err)
				}
				revoked = false // already revoked on the server
			}
			for _, store := range []string{nsync.StoreKeyring, nsync.StoreFile} {
				if store == nsync.StoreKeyring && os.Getenv(nsync.EnvKeyring) == nsync.StoreFile {
					continue
				}
				if err := nsync.DeleteToken(acc.key(), store); err != nil {
					a.log.Warn("could not delete the stored token", "store", store, "err", err)
				}
			}
			if a.json {
				return a.printJSON(map[string]any{"server": acc.URL, "device": acc.Device, "revoked": revoked})
			}
			fmt.Fprintf(a.stdout, "signed out of %s (device %s)\n", acc.URL, acc.Device)
			return nil
		},
	}
	cmd.Flags().StringVar(&device, "device", "", "the device name used at login")
	return cmd
}

func (a *app) devicesCmd() *cobra.Command {
	var server, device string
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "List the devices signed in to your account",
		Args:  exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			acc, err := a.resolveAccount(server, device)
			if err != nil {
				return err
			}
			c, err := a.accountClient(acc)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			ds, err := c.Devices(ctx)
			if err != nil {
				return syncErr(err)
			}
			if a.json {
				return a.printJSON(map[string]any{"server": acc.URL, "devices": ds})
			}
			tw := tabwriter.NewWriter(a.stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tDEVICE\tSIGNED IN\tLAST USED\t")
			for _, d := range ds {
				used, cur := "never", ""
				if d.LastUsedAt != nil {
					used = d.LastUsedAt.Format(time.RFC3339)
				}
				if d.Current {
					cur = "(this device)"
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", d.ID, d.Device, d.CreatedAt.Format(time.RFC3339), used, cur)
			}
			return tw.Flush()
		},
	}
	pf := cmd.PersistentFlags()
	pf.StringVar(&server, "server", "", "server URL (default: the vault's remote, $NOTA_SERVER_URL or "+DefaultServer+")")
	pf.StringVar(&device, "device", "", "the device name used at login")
	revoke := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Sign a device out (revoke its token)",
		Args:  exactArgs(1, "a device id (see nota devices)"),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return usageErr("bad device id %q", args[0])
			}
			acc, err := a.resolveAccount(server, device)
			if err != nil {
				return err
			}
			c, err := a.accountClient(acc)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			if err := c.RevokeDevice(ctx, id); err != nil {
				if nsync.IsCode(err, protocol.CodeNotFound) {
					return &Error{Code: ExitNotFound, Kind: "not_found", Message: fmt.Sprintf("no device %d on your account", id)}
				}
				return syncErr(err)
			}
			if a.json {
				return a.printJSON(map[string]any{"revoked": id})
			}
			fmt.Fprintf(a.stdout, "revoked device %d\n", id)
			return nil
		},
	}
	cmd.AddCommand(revoke)
	return cmd
}
