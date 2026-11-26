export const site = {
  name: "Nota",
  url: "https://nota.banjoanton.com",
  repo: "https://github.com/banjo/nota",
  releases: "https://github.com/banjo/nota/releases",
  releasesFeed: "https://github.com/banjo/nota/releases.atom",
  readme: "https://github.com/banjo/nota#readme",
  deployDoc: "https://github.com/banjo/nota/blob/HEAD/docs/deploy.md",
  apiDoc: "https://github.com/banjo/nota/blob/HEAD/docs/api.md",
  syncDoc: "https://github.com/banjo/nota/blob/HEAD/docs/sync-protocol.md",
  license: "https://github.com/banjo/nota/blob/HEAD/LICENSE",
  title: "Nota: your notes are files",
  description:
    "Local Markdown notes in a small Go app, for desktop and the web, synced through a server you run. A CLI and an MCP server work on the same folder.",
  goInstall: "go install github.com/banjo/nota/cmd/nota@latest",
  signIn: "/app/",
  brewCask: "brew install --cask banjo/tap/nota",
  claudeMcp: "claude mcp add nota -- nota mcp",
};

/**
 * The numbers in the "light" row, measured on Linux amd64 (Go 1.27.4)
 * from origin/main 4d6762f. Commands and raw output are in the commit
 * that added them; re-measure before changing a value.
 */
export const measured = [
  // CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' ./cmd/nota → 19,832,992 bytes
  { value: "20", unit: "MB", label: "CLI, MCP and server, one binary" },
  // cd apps/desktop && bunx mygo build → build/linux-amd64/nota, 20,242,695 bytes
  { value: "21", unit: "MB", label: "Desktop app, no bundled browser" },
  // nota serve, empty data dir, ps -o rss after startup → 18,596 KiB (19.0 MB)
  { value: "20", unit: "MB", label: "Idle server memory" },
  // go test -bench Search10k ./internal/index → 17,450,094 ns/op
  { value: "18", unit: "ms", label: "Search, 10,000 notes" },
];

/** The MCP tools `nota mcp` and the server's /mcp expose (internal/mcp). */
export const mcpTools: [name: string, what: string][] = [
  ["search_notes", "full-text search, with snippets"],
  ["read_note", "a note, its frontmatter, links and hash"],
  ["list_notes", "a folder, a tag, a where clause or a view"],
  ["create_note", "a new note, in the inbox by default"],
  ["append_to_note", "adds text to the end of a note"],
  ["update_note", "a body, refused if the note moved on"],
  ["update_frontmatter", "properties and tags, line by line"],
  ["replace_in_notes", "find and replace, shown before it writes"],
  ["move_note", "files a note into a folder"],
  ["list_tags", "every tag with its count"],
  ["list_views", "the saved views in .nota/views"],
  ["get_backlinks", "the notes that link to a note"],
];

/**
 * The prompt in "Hand this to your agent". The cask installs the app and
 * puts `nota` on PATH, so it replaces go install once a release is out.
 */
export function agentPrompt(released: boolean): string {
  const install = released
    ? "Run `brew install --cask banjo/tap/nota` (Apple silicon Mac; it installs the app and the `nota` CLI) and check that `nota` is on my PATH."
    : "Run `go install github.com/banjo/nota/cmd/nota@latest` and check that `nota` is on my PATH.";
  return `Install Nota, a Markdown notes app that keeps notes as .md files in a folder, and connect it to yourself:

1. ${install}
2. Use ~/Notes as the vault, or ask me which folder I keep notes in. Run \`nota ls\` to check it.
3. Add the MCP server: \`claude mcp add nota -- nota mcp\`.
4. Read https://nota.banjoanton.com/llms.txt for the commands and tools.`.replace(/\\`/g, "`");
}

export function agentCommands(released: boolean): string {
  return `${released ? site.brewCask : site.goInstall}\n${site.claudeMcp}`;
}

/** A transform hook (internal/hooks, .nota/hooks.toml). */
export const hookExample = `# Inbox notes tagged #recipe go to Recipes/
[[hook]]
event = "inbox.added"
mode = "transform"
match = { tag = "recipe" }
run = "echo '{\\"folder\\": \\"Recipes\\"}'"`;

/** docs/deploy.md: without RP_ID, WEB_ORIGIN and COOKIE_DOMAIN passkeys fail. */
export const docker = `docker build -t nota https://github.com/banjo/nota.git
docker run -d --name nota -p 8080:8080 -v nota-data:/data \\
  -e NOTA_SERVER_PUBLIC_URL=https://api.example.com \\
  -e NOTA_SERVER_WEB_ORIGIN=https://notes.example.com \\
  -e NOTA_SERVER_RP_ID=notes.example.com \\
  -e NOTA_SERVER_COOKIE_DOMAIN=example.com \\
  nota
docker exec nota nota serve user add you@example.com`;
