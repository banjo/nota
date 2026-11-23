import type { APIRoute } from "astro";
import { fetchRelease } from "../data/release.ts";
import { mcpTools, site } from "../data/site.ts";

/** /llms.txt: what Nota is, for a model reading the site. */
export const GET: APIRoute = async () => {
  const release = await fetchRelease();
  const app = release
    ? `- Desktop app ${release.version}: ${release.url}. Homebrew: \`${site.brewCask}\``
    : `- The desktop app ships with the first release, which is not out yet: ${site.releasesFeed}. The CLI and the MCP server work today.`;

  const body = `# Nota

> Nota is a local-first Markdown notes app. Notes are \`.md\` files in a folder (the vault); a SQLite index for search, tags and backlinks sits beside them and can be rebuilt. One small Go codebase gives a desktop app (macOS, Linux; system webview), a web app, a CLI and an MCP server. Sync is optional, to a server you run.

${app}
- CLI and MCP server: \`${site.goInstall}\` (Go 1.27+)
- Free and open source, MIT: ${site.repo}
- A vault is a folder: \`--vault\`, \`$NOTA_VAULT\`, else ~/Notes. New notes land in \`Inbox/\`.
- Frontmatter is the metadata (tags, status, created/updated, custom keys). Edits are line-based, unknown keys survive.

## CLI

Every command takes \`--json\`.

- \`nota add "text" [--folder F] [--tag T]\`: a new note
- \`nota ls [--tag T] [--folder F] [--where k=v] [--view V]\`: list notes
- \`nota search <query>\`, \`nota cat <note>\`, \`nota open <note>\`
- \`nota set <note> key=value\`, \`nota tag <note> +tag -tag\`, \`nota mv <note> <folder>\`, \`nota rm <note>\`
- \`nota inbox\`, \`nota views\`, \`nota tags\`, \`nota backlinks <note>\`
- \`nota index --rebuild\`: rebuild the index from the files
- \`nota sync [--watch]\`, \`nota remote add <url>\`, \`nota login\`, \`nota history <note>\`, \`nota restore <note> <version>\`
- \`nota serve\`: the sync server, web API and HTTP MCP
- \`nota mcp\`: an MCP server on stdio

## MCP

Claude Code: \`${site.claudeMcp}\`. Other clients: command \`nota\`, args \`["mcp"]\`. On a server the same tools answer at \`POST /mcp\` with a device token.

${mcpTools.map(([name, what]) => `- \`${name}\`: ${what}`).join("\n")}

## Plugins

Plugins live in the vault's \`.nota/\` folder and sync with it.

- Views: \`.nota/views/*.yaml\` are saved queries shown as virtual folders (\`name\`, \`where\`, \`tag\`, \`folder\`, \`sort\`, \`limit\`). \`nota ls --view <id>\`.
- Hooks: \`.nota/hooks.toml\` runs shell commands on \`note.created\`, \`note.saved\`, \`note.moved\`, \`note.deleted\`, \`inbox.added\`, \`sync.completed\`, with the event as JSON on stdin. \`transform\` hooks (on \`note.created\` and \`inbox.added\`) print a patch such as \`{"folder": "Recipes", "tags": {"add": ["x"]}, "set": {"status": "todo"}}\`. The server runs them too with \`NOTA_SERVER_HOOKS=on\`. \`nota hooks list\`, \`nota hooks test <event> <note>\`.
- UI plugins (commands, panels, editor widgets with a manifest and permissions) are planned, not released.

## Sync

\`nota serve\` is one Go binary with SQLite and a blob folder. Accounts use passkeys and recovery codes. Sync is file-level with a three-way merge and conflict copies. No end-to-end encryption: the server can read the notes it stores.

## Links

- [README](${site.readme})
- [Deploying the server](${site.deployDoc})
- [Web API](${site.apiDoc})
- [Sync protocol](${site.syncDoc})
`;

  return new Response(body, {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};
