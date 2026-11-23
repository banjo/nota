package cli

import (
	"io"
	"os"
	"os/signal"
	"syscall"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/banjo/nota/internal/mcp"
)

func (a *app) mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve the vault to AI agents over MCP (stdio)",
		Long: `Run a Model Context Protocol server on stdin/stdout, for agents such as
Claude Code or any other MCP client. Configure it as {"command": "nota", "args": ["mcp"]};
add "--vault", "DIR" to the args to pick a vault. Logs go to stderr.

Tools: search_notes, read_note, list_notes, create_note, append_to_note,
update_note, update_frontmatter, move_note, list_tags, list_views,
get_backlinks. Notes are also resources at nota://note/<path>. Hooks in
.nota/hooks.toml run on the server's writes.`,
		Args: exactArgs(0, "no arguments"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := a.openVault(true); err != nil {
				return err
			}
			ix, err := a.index()
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			srv := mcp.NewServer(mcp.NewService(a.vault, ix), Version, a.log)
			a.log.Debug("mcp: serving on stdio", "vault", a.vault.Root())
			err = srv.Run(ctx, &sdk.IOTransport{Reader: io.NopCloser(a.stdin), Writer: nopWriteCloser{a.stdout}})
			if ctx.Err() != nil || err == nil || err == io.EOF {
				return nil // the client went away or we were stopped
			}
			return err
		},
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }
