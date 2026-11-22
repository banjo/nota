package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	notamcp "github.com/banjo/nota/internal/mcp"
	"github.com/banjo/nota/internal/protocol"
)

// MCP over HTTP at /mcp: the same tools as `nota mcp` (internal/mcp),
// for agents that reach the server instead of a local vault. A request
// authenticates with a device token and works on one of the user's
// vaults: ?vault=name or the Nota-Vault header, else the user's first
// vault. The tools run on the vault's working copy and index; their
// writes are versions of the token's device in the log.
//
// The transport is the SDK's Streamable HTTP in stateless mode: every
// POST is handled on its own, so a session can never be picked up by
// another user's token.

// HeaderVault picks the vault of an /mcp request.
const HeaderVault = "Nota-Vault"

// MCPPath is where the HTTP MCP endpoint is mounted.
const MCPPath = "/mcp"

type mcpCtxKey struct{}

// mcpTarget is what getServer needs to build the request's MCP server.
type mcpTarget struct {
	ws     *workspace
	device string
}

func (s *Server) mcpRoutes() {
	h := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		t, ok := r.Context().Value(mcpCtxKey{}).(mcpTarget)
		if !ok {
			return nil
		}
		ed, err := t.ws.editor(t.device)
		if err != nil {
			s.log.Error("mcp: open the vault", "err", err)
			return nil
		}
		svc := notamcp.NewServiceLocked(ed, t.ws.ix, &t.ws.mu)
		return notamcp.NewServer(svc, s.cfg.Version, s.log.With("component", "mcp"))
	}, &sdk.StreamableHTTPOptions{Stateless: true, Logger: s.log.With("component", "mcp")})
	s.mux.Handle(MCPPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target, err := s.mcpTarget(r)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="nota"`)
			s.writeErr(w, r, err)
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), mcpCtxKey{}, target)))
	}))
}

// mcpTarget authenticates an /mcp request and opens its vault.
func (s *Server) mcpTarget(r *http.Request) (mcpTarget, error) {
	p, err := s.authenticate(r)
	if err != nil {
		return mcpTarget{}, err
	}
	ri := info(r)
	ri.user, ri.device = p.Email, p.Device
	ri.attrs = append(ri.attrs, "auth", "token")
	name := r.URL.Query().Get("vault")
	if name == "" {
		name = strings.TrimSpace(r.Header.Get(HeaderVault))
	}
	ctx := r.Context()
	if name == "" {
		err := s.store.db.QueryRowContext(ctx, `SELECT name FROM vaults WHERE user_id = ? ORDER BY id LIMIT 1`, p.UserID).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return mcpTarget{}, &apiErr{http.StatusNotFound, protocol.Error{Code: protocol.CodeNotFound,
				Message: "you have no vault on this server yet; sync one first (nota remote add)"}}
		}
		if err != nil {
			return mcpTarget{}, err
		}
	}
	ri.vault = name
	ws, err := s.workspace(ctx, p.UserID, name)
	if err != nil {
		return mcpTarget{}, err
	}
	return mcpTarget{ws: ws, device: p.Device}, nil
}
