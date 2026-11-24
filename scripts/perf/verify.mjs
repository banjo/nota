// Behaviour checks for the perf work, in a real browser against the perf
// build (scripts/perf/dist), plus full-resolution screenshots. Each check
// throws on failure; the run prints one line per check.
//
//   PLAYWRIGHT=… CHROME=… bun scripts/perf/verify.mjs [--shots DIR] [--only name,name]

import { existsSync, mkdirSync, readFileSync, statSync } from "node:fs";
import { createServer } from "node:http";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const argv = process.argv.slice(2);
const opt = (k) => {
  const i = argv.indexOf(`--${k}`);
  return i >= 0 ? argv[i + 1] : undefined;
};
const SHOTS = opt("shots") ? resolve(opt("shots")) : null;
const ONLY = opt("only") ? new Set(opt("only").split(",")) : null;
const DIST = opt("dist") ? resolve(opt("dist")) : join(here, "dist");
if (SHOTS) mkdirSync(SHOTS, { recursive: true });
const { chromium } = await import(process.env.PLAYWRIGHT ?? "playwright");

const TYPES = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css" };
const server = createServer((req, res) => {
  let file = join(DIST, decodeURIComponent(new URL(req.url, "http://x").pathname));
  if (!existsSync(file) || statSync(file).isDirectory()) file = join(DIST, "index.html");
  res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" });
  res.end(readFileSync(file));
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const BASE = `http://127.0.0.1:${server.address().port}/`;
const browser = await chromium.launch({
  executablePath: process.env.CHROME || undefined,
  args: ["--disable-gpu", "--no-sandbox"],
});

const expect = (cond, msg) => {
  if (!cond) throw new Error(msg);
};
async function open(url = "", storage = {}) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  await context.addInitScript((kv) => {
    for (const [k, v] of Object.entries(kv)) localStorage.setItem(k, v);
  }, storage);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  await page.goto(BASE + url);
  await page.locator(".cm-content").first().waitFor();
  await page.waitForFunction(() => document.querySelector('textarea[aria-label="Title"]')?.value);
  await page.waitForTimeout(500); // the first queries settle
  return { context, page, errors };
}
const shot = async (page, name, clip) => {
  if (SHOTS) await page.screenshot({ path: join(SHOTS, `${name}.png`), ...(clip ? { clip } : {}) });
};
const tree = (page) => page.locator('[role="tree"]');
const noteRow = (page, path) =>
  tree(page).locator(`button[data-ctx-kind="note"][data-ctx-path="${path}"]`);
const folderRow = (page, path) =>
  tree(page).locator(`button[data-ctx-kind="folder"][data-ctx-path="${path}"]`);
const active = (page) =>
  page.evaluate(() => {
    const a = document.activeElement;
    return a ? { label: a.getAttribute("aria-label"), tag: a.tagName, text: a.textContent } : null;
  });

/** Radix puts the text in a visually hidden role=tooltip node. */
const tooltip = (page, text) =>
  page.waitForFunction(
    (t) => [...document.querySelectorAll('[role="tooltip"]')].some((e) => e.textContent === t),
    text,
    { timeout: 3000 },
  );

const CHECKS = {
  // The tree's "…" menu mounts on first pointer: hover, click, it opens.
  async "tree menu by pointer"(page) {
    const row = noteRow(page, "Meetings/Standup.md").locator("..");
    await row.hover();
    const trigger = row.getByRole("button", { name: "Note actions" });
    await trigger.hover();
    await trigger.click();
    const menu = page.getByRole("menu");
    await menu.waitFor();
    expect(await menu.getByRole("menuitem", { name: "Move to…" }).isVisible(), "no Move to…");
    await shot(page, "tree-menu");
    await page.keyboard.press("Escape");
    await menu.waitFor({ state: "detached" });
  },
  // Tab from the row reaches the "…" button, which keeps focus once the
  // real trigger mounts; Enter opens the menu, ArrowDown moves in it.
  async "tree menu by keyboard"(page) {
    await noteRow(page, "Meetings/Standup.md").focus();
    await page.keyboard.press("Tab");
    await page.waitForTimeout(50);
    const a = await active(page);
    expect(a?.label === "Note actions", `focus after Tab: ${JSON.stringify(a)}`);
    const visible = await page.evaluate(() => document.activeElement.matches(":focus-visible"));
    expect(visible, "trigger is not :focus-visible after Tab");
    await page.keyboard.press("Enter");
    await page.getByRole("menu").waitFor();
    await page.keyboard.press("ArrowDown");
    const item = await active(page);
    expect(
      item?.text?.includes("Move to") || item?.text?.includes("trash"),
      `menu focus: ${JSON.stringify(item)}`,
    );
    await page.keyboard.press("Escape");
    await page.getByRole("menu").waitFor({ state: "detached" });
    // (Where focus lands after Esc is the same as before the perf work:
    // Radix hands it back to the trigger, which then hides; not asserted.)
  },
  // "Move to…" from the tree menu opens the palette in folder mode.
  async "tree menu runs its action"(page) {
    const row = noteRow(page, "Meetings/Standup.md").locator("..");
    await row.hover();
    await row.getByRole("button", { name: "Note actions" }).click();
    await page.getByRole("menuitem", { name: "Move to…" }).click();
    await page.locator("[cmdk-input]").waitFor();
    const ph = await page.locator("[cmdk-input]").getAttribute("placeholder");
    expect(ph?.startsWith("Add “Standup” to folder"), `palette placeholder: ${ph}`);
    await page.keyboard.press("Escape");
  },
  // Folder rows: hover buttons keep their tooltips, by pointer and by focus.
  async "folder row tooltips"(page) {
    await folderRow(page, "Projects").hover();
    const btn = tree(page).getByRole("button", { name: "New note in Projects" });
    await btn.hover();
    await page.mouse.move(1, 1, { steps: 1 });
    await btn.hover();
    await tooltip(page, "New note in Projects");
    await shot(page, "folder-tooltip");
    await page.mouse.move(800, 600);
    // Keyboard: Tab from the folder row to its first action.
    await folderRow(page, "Areas").focus();
    await page.keyboard.press("Tab");
    await page.waitForTimeout(50);
    const a = await active(page);
    expect(a?.label === "New note in Areas", `focus after Tab: ${JSON.stringify(a)}`);
    await tooltip(page, "New note in Areas");
    await page.keyboard.press("Tab");
    const b = await active(page);
    expect(b?.label === "New folder in Areas", `second Tab: ${JSON.stringify(b)}`);
    // Enter on the focused action runs it: the new-folder draft row appears.
    await page.keyboard.press("Enter");
    await page.getByRole("textbox", { name: "New folder name" }).waitFor();
    await page.keyboard.press("Escape");
  },
  // Clicking a folder's "New note in" creates a note there and opens it.
  async "folder row new note"(page) {
    await folderRow(page, "Reading").hover();
    const btn = tree(page).getByRole("button", { name: "New note in Reading" });
    await btn.hover();
    await btn.click();
    await page.waitForFunction(() =>
      document.querySelector('[role="tree"] button[data-ctx-path^="Reading/Untitled"]'),
    );
  },
  // ⌘K: title search, Enter opens the note.
  async "palette search and open"(page) {
    await page.keyboard.press("Control+k");
    await page.locator("[cmdk-input]").waitFor();
    await page.keyboard.type("Note 05", { delay: 30 });
    const hit = page.locator("[cmdk-item]", { hasText: "Note 05" }).first();
    await hit.waitFor();
    await shot(page, "palette-search");
    // (Enter right after typing selects nothing, before and after the perf
    // work: the deferred list replaces the item cmdk had selected.)
    await hit.click();
    await page.locator("[cmdk-input]").waitFor({ state: "detached" });
    await page.waitForFunction(
      () => document.querySelector('textarea[aria-label="Title"]').value === "Note 05",
    );
  },
  // Each open starts empty, also after a query was left in it.
  async "palette query resets"(page) {
    await page.keyboard.press("Control+k");
    await page.locator("[cmdk-input]").waitFor();
    await page.keyboard.type("zzz", { delay: 20 });
    await page.keyboard.press("Escape");
    await page.locator("[cmdk-input]").waitFor({ state: "detached" });
    // The command runner ignores the same command again within 300 ms.
    await page.waitForTimeout(350);
    await page.keyboard.press("Control+k");
    await page.locator("[cmdk-input]").waitFor();
    const v = await page.locator("[cmdk-input]").inputValue();
    expect(v === "", `query kept: ${JSON.stringify(v)}`);
    // Recent notes and commands show with no query.
    await page.getByRole("option", { name: "Toggle dark mode" }).waitFor();
    expect(
      await page.locator("[cmdk-group-heading]", { hasText: "Recent" }).isVisible(),
      "no Recent group",
    );
  },
  // Arrow keys move the selection; Enter runs the selected command.
  async "palette arrows and commands"(page) {
    await page.keyboard.press("Control+k");
    await page.keyboard.type("dark mode", { delay: 20 });
    await page.getByRole("option", { name: "Toggle dark mode" }).waitFor();
    const sel = () =>
      page.evaluate(() => document.querySelector('[cmdk-item][aria-selected="true"]')?.textContent);
    const first = await sel();
    await page.keyboard.press("ArrowDown");
    const second = await sel();
    expect(first !== second, `ArrowDown kept ${first}`);
    await page.keyboard.press("ArrowUp");
    expect((await sel()) === first, "ArrowUp did not go back");
    const before = await page.evaluate(() => document.documentElement.dataset.mode);
    await page.getByRole("option", { name: "Toggle dark mode" }).click();
    await page.waitForFunction((b) => document.documentElement.dataset.mode !== b, before);
    await shot(page, "dark");
  },
  // A mode switch while open (Add to folder… from ⌘K) starts that mode fresh.
  async "palette folder mode"(page) {
    await page.keyboard.press("Control+k");
    await page.keyboard.type("Add to folder", { delay: 20 });
    await page.getByRole("option", { name: "Add to folder… Ctrl+Shift+M" }).click();
    const input = page.locator("[cmdk-input]");
    await page.waitForFunction(() =>
      document.querySelector("[cmdk-input]")?.placeholder?.startsWith("Add “Standup”"),
    );
    expect((await input.inputValue()) === "", "query not reset on mode change");
    await page
      .getByRole("option", { name: /Archive/ })
      .first()
      .waitFor();
    await shot(page, "palette-folders");
    await page.keyboard.type("Arch", { delay: 20 });
    await page
      .getByRole("option", { name: /Archive/ })
      .first()
      .click();
    await page.waitForFunction(() =>
      document.querySelector('[role="tree"] button[data-ctx-path="Archive/Standup.md"]'),
    );
  },
  // The sidebar's New note goes to the inbox (from the tree's inbox name).
  async "sidebar new note"(page) {
    await page
      .getByRole("button", { name: /^New note/ })
      .first()
      .click();
    await page.waitForFunction(async () => {
      const t = await window.__notaClient.tree();
      return t.notes.some((n) => n.path.startsWith("Inbox/Untitled"));
    });
  },
  // Right-click on a folder: its actions, Rename included (not the inbox).
  async "folder context menu"(page) {
    await folderRow(page, "Archive").click({ button: "right" });
    const menu = page.getByRole("menu");
    await menu.waitFor();
    for (const name of ["New note here", "New subfolder", "Rename folder"])
      await menu.getByRole("menuitem", { name }).waitFor();
    await shot(page, "folder-context-menu");
    await page.keyboard.press("Escape");
  },
};

let failed = 0;
for (const [name, fn] of Object.entries(CHECKS)) {
  if (ONLY && !ONLY.has(name)) continue;
  const { context, page, errors } = await open();
  try {
    await fn(page);
    expect(errors.length === 0, `page errors: ${errors.join(" | ")}`);
    console.log(`ok   ${name}`);
  } catch (e) {
    failed++;
    console.log(`FAIL ${name}: ${String(e.message ?? e).split("\n")[0]}`);
    await shot(page, `fail-${name.replace(/\W+/g, "-")}`);
  }
  await context.close();
}
await browser.close();
server.close();
process.exit(failed ? 1 : 0);
