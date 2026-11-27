package mcp

import (
	"context"
	"errors"
	"log/slog"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/banjo/nota/internal/vault"
)

// URITemplate is the resource template of notes. {+path} keeps the
// slashes of folder paths readable; percent-encoded paths work too.
const URITemplate = "nota://note/{+path}"

const instructions = `Nota is a vault of plain markdown notes with optional YAML frontmatter
(tags, status, created/updated, custom keys). A note's identity is its
vault-relative path, e.g. "Projekt/Plan.md"; tools also accept a title or a
unique prefix. New notes go to the inbox folder unless a folder is given.
Edits keep every byte they don't touch. To rewrite a note safely, read it,
then pass its hash as expected_hash to update_note.`

// NewServer returns an MCP server with the vault tools and the note
// resources. Serve it on any transport, e.g. server.Run(ctx, &sdk.StdioTransport{}).
func NewServer(s *Service, version string, logger *slog.Logger) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "nota", Title: "Nota notes", Version: version},
		&sdk.ServerOptions{Instructions: instructions, Logger: logger})

	ro := &sdk.ToolAnnotations{ReadOnlyHint: true}
	idem := &sdk.ToolAnnotations{IdempotentHint: true, DestructiveHint: ptr(false)}
	add := &sdk.ToolAnnotations{DestructiveHint: ptr(false)}

	tool(srv, "search_notes", "Full-text search over note titles and bodies, best matches first, with a snippet of the matching text.", ro, s.Search)
	tool(srv, "read_note", "Read one note: its frontmatter properties, tags, body, the whole file and a content hash.", ro, s.Read)
	tool(srv, "list_notes", "List notes, filtered by folder, tag, frontmatter conditions (where), pinned or a saved view, sorted by path unless sort is given.", ro, s.List)
	tool(srv, "create_note", "Create a note. It goes to the inbox unless folder is given; the file name comes from the title. Tags and properties go into the frontmatter, with a created timestamp. Returns the note where it ended up (hooks may file it).", add, s.Create)
	tool(srv, "append_to_note", "Append markdown to the end of a note, on a new line. Use it for logs, lists and adding to a note without rewriting it.", add, s.Append)
	tool(srv, "update_note", "Replace the body of a note (everything after the frontmatter, which is kept). Pass expected_hash from read_note to fail instead of overwriting changes made since.", idem, s.Update)
	tool(srv, "update_frontmatter", "Set and remove frontmatter keys of a note. Only those keys' lines change; the updated timestamp is kept current.", idem, s.Frontmatter)
	tool(srv, "replace_in_notes", "Find and replace text across notes, in bodies only (frontmatter is never touched; use update_frontmatter). Code blocks and inline code are skipped unless include_code. A dry run by default: it lists each match with the line around it and what it becomes; call again with dry_run false to write. Each changed note is saved as an edit.", &sdk.ToolAnnotations{DestructiveHint: ptr(true)}, s.Replace)
	tool(srv, "move_note", "Move a note into another folder, keeping its file name. Links to it that would break are rewritten (only their targets change). Fails if a note with that name is already there.", idem, s.Move)
	tool(srv, "create_folder", "Create an empty folder (a/b makes both). Fails if it exists. Notes can also go into a new folder directly: create_note and move_note create the folder.", add, s.CreateFolder)
	tool(srv, "list_folders", "List every folder, empty ones too, with the number of notes in it (subfolders included).", ro,
		func(struct{}) (FoldersOutput, error) { return s.Folders() })
	tool(srv, "set_folder_icon", "Give a folder an icon and a colour in the sidebar (both empty take them away). Find icon ids with search_icons. Kept in .nota/folders.yaml, which syncs.", idem, s.SetFolderIcon)
	tool(srv, "search_icons", "Find icons for folders and saved views by name: Lucide, Tabler, emoji and Simple Icons (brand logos), best match first.", ro, s.SearchIcons)
	tool(srv, "list_trash", "List the deleted notes in the vault's .trash/, most recently deleted first, with where each was deleted from and when.", ro,
		func(struct{}) (TrashOutput, error) { return s.Trash() })
	tool(srv, "restore_note", "Put a deleted note back where it was deleted from, or into another folder. Fails if a note has taken that path since; pass folder then.", add, s.Restore)
	tool(srv, "list_tags", "List every tag in the vault (frontmatter and inline #tags) with the number of notes that have it.", ro,
		func(struct{}) (TagsOutput, error) { return s.Tags() })
	tool(srv, "list_views", "List the saved views (.nota/views/*.yaml): named queries with their note counts. Pass a view's id to list_notes.", ro,
		func(struct{}) (ViewsOutput, error) { return s.Views() })
	tool(srv, "get_backlinks", "List the notes that link to a note ([[wikilinks]], embeds and markdown links).", ro, s.Backlinks)

	srv.AddResourceTemplate(&sdk.ResourceTemplate{
		Name:        "note",
		Title:       "Note",
		Description: "A markdown note by vault path, e.g. nota://note/Projekt/Plan.md",
		MIMEType:    "text/markdown",
		URITemplate: URITemplate,
	}, func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
		uri := req.Params.URI
		p, err := PathFromURI(uri)
		if err != nil {
			return nil, sdk.ResourceNotFoundError(uri)
		}
		n, err := s.ReadPath(p)
		if errors.Is(err, vault.ErrNotFound) {
			return nil, sdk.ResourceNotFoundError(uri)
		}
		if err != nil {
			return nil, err
		}
		return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{
			{URI: URI(n.Path), MIMEType: "text/markdown", Text: n.Content},
		}}, nil
	})
	return srv
}

// tool registers fn as a tool. Its input and output schemas come from
// the Go types; an error becomes a tool error the model can read.
func tool[In, Out any](srv *sdk.Server, name, desc string, ann *sdk.ToolAnnotations, fn func(In) (Out, error)) {
	sdk.AddTool(srv, &sdk.Tool{Name: name, Description: desc, Annotations: ann},
		func(_ context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, Out, error) {
			out, err := fn(in)
			return nil, out, err
		})
}

func ptr[T any](v T) *T { return &v }
