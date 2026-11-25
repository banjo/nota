// Render-count harness: drives the perf build (scripts/perf/dist) in
// Chromium through each scenario and records React commits, component
// renders by name, long tasks and scripting time. See docs/perf.md.
//
//   PLAYWRIGHT=/tmp/pw/node_modules/playwright/index.mjs \
//   CHROME=~/.cache/ms-playwright/chromium-1255/chrome-linux64/chrome \
//   bun scripts/perf/run.mjs --out perf/baseline.json [--runs 3] [--only a,b]

import { readFileSync, writeFileSync, existsSync, statSync } from "node:fs";
import { createServer } from "node:http";
import { dirname, extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { execSync } from "node:child_process";

const here = dirname(fileURLToPath(import.meta.url));
const args = Object.fromEntries(
  process.argv
    .slice(2)
    .join(" ")
    .split(/\s*--/)
    .filter(Boolean)
    .map((a) => {
      const [k, ...v] = a.split(/\s+/);
      return [k, v.join(" ") || "1"];
    }),
);
const RUNS = Number(args.runs ?? 3);
const OUT = args.out ? resolve(args.out) : null;
const ONLY = args.only ? new Set(args.only.split(",")) : null;
const DIST = resolve(args.dist ?? join(here, "dist"));
const { chromium } = await import(process.env.PLAYWRIGHT ?? "playwright");

// ---- static server for the build ----
const TYPES = {
  ".html": "text/html",
  ".js": "text/javascript",
  ".css": "text/css",
  ".svg": "image/svg+xml",
};
const server = createServer((req, res) => {
  const url = new URL(req.url, "http://x");
  let file = join(DIST, decodeURIComponent(url.pathname));
  if (!existsSync(file) || statSync(file).isDirectory()) file = join(DIST, "index.html");
  res.writeHead(200, { "content-type": TYPES[extname(file)] ?? "application/octet-stream" });
  res.end(readFileSync(file));
});
await new Promise((r) => server.listen(0, "127.0.0.1", r));
const BASE = `http://127.0.0.1:${server.address().port}/`;

const HOOK = readFileSync(join(here, "hook.js"), "utf8");
const browser = await chromium.launch({
  executablePath: process.env.CHROME || undefined,
  args: ["--disable-gpu", "--no-sandbox"],
});

// ---- helpers ----
/** Waits until no commit for `quiet` ms, counted from the later of the last commit and now. */
async function settle(page, quiet = 1000, max = 15000) {
  const t0 = Date.now();
  const from = await page.evaluate(() => performance.now());
  for (;;) {
    const since = await page.evaluate(
      (f) => performance.now() - Math.max(f, window.__perf.lastCommitAt),
      from,
    );
    if (since >= quiet || Date.now() - t0 > max) return;
    await page.waitForTimeout(Math.min(quiet - since + 20, 250));
  }
}
const title = (page) => page.locator('textarea[aria-label="Title"]');
async function ready(page, want) {
  await page.locator(".cm-content").first().waitFor();
  await page.waitForFunction(
    (w) => {
      const t = document.querySelector('textarea[aria-label="Title"]');
      return t && t.value && (!w || t.value === w);
    },
    want,
    { timeout: 15000 },
  );
}
const tree = (page) => page.locator('[role="tree"]');
const folderRow = (page, path) =>
  tree(page).locator(`button[data-ctx-kind="folder"][data-ctx-path="${path}"]`);
const noteRow = (page, path) =>
  tree(page).locator(`button[data-ctx-kind="note"][data-ctx-path="${path}"]`);
async function expand(page, paths) {
  for (const p of paths) {
    const row = folderRow(page, p);
    await row.waitFor();
    if ((await row.getAttribute("aria-expanded")) !== "true") await row.click();
  }
}
const MOD = "Control";
/** The text reached the client (the debounced save ran). */
async function saved(page, path, text) {
  const ok = await page.evaluate(
    async ([p, t]) => (await window.__notaClient.get(p)).body.includes(t),
    [path, text],
  );
  if (!ok) throw new Error(`not saved: ${path} lacks ${JSON.stringify(text)}`);
}
async function paletteRun(page, query, option) {
  await page.keyboard.press(`${MOD}+k`);
  await page.locator("[cmdk-input]").waitFor();
  await page.keyboard.type(query, { delay: 40 });
  const opt = page.getByRole("option", { name: option, exact: true });
  await opt.waitFor();
  await opt.click();
  await page.locator("[cmdk-input]").waitFor({ state: "detached" });
}
async function openTen(page, folders) {
  await expand(page, folders);
  const rows = tree(page).locator('button[data-ctx-kind="note"]');
  await rows.first().waitFor();
  const current = await title(page).inputValue();
  const all = await rows.evaluateAll((els) =>
    els.map((e) => [e.dataset.ctxPath, e.dataset.ctxTitle]),
  );
  return all.filter(([, t]) => t !== current).slice(0, 10);
}

// ---- scenarios: setup (not measured) and run (measured) ----
const SMALL_FOLDERS = ["Projects", "Projects/Website", "Areas", "Reading", "Archive"];
// Bottom-up, so opening one never pushes the next out of the virtualized window.
const LARGE_FOLDERS = ["Area 5", "Area 5/Topic 0", "Area 4", "Area 3", "Area 2"];
const SCENARIOS = [
  {
    id: "a",
    name: "cold load → first note visible",
    cold: true,
    run: async (page, m) => {
      const t0 = Date.now();
      await page.goto(BASE);
      await ready(page);
      m.readyMs = Date.now() - t0;
    },
  },
  {
    id: "b",
    name: "open 10 notes via sidebar",
    setup: async (page, s) => (s.rows = await openTen(page, SMALL_FOLDERS)),
    run: async (page, m, s) => {
      for (const [path, t] of s.rows) {
        await noteRow(page, path).click();
        await ready(page, t);
      }
    },
  },
  {
    id: "b-2000",
    name: "open 10 notes via sidebar (2000 notes)",
    url: "?vault=large",
    setup: async (page, s) => (s.rows = await openTen(page, ["Area 0"])),
    run: async (page, m, s) => {
      for (const [path, t] of s.rows) {
        await noteRow(page, path).click();
        await ready(page, t);
      }
    },
  },
  {
    id: "c",
    name: "type 60 chars in a note",
    setup: async (page) => {
      await page.locator(".cm-content .cm-line").nth(2).click();
      await page.keyboard.press("End");
    },
    run: async (page) => {
      await page.keyboard.type(" quick brown fox jumps over the lazy dog and keeps on typing", {
        delay: 30,
      });
    },
    check: (page) => saved(page, "Meetings/Standup.md", "lazy dog and keeps on typing"),
  },
  {
    id: "d",
    name: "⌘K: open, type query, ↓×5, close",
    run: async (page) => {
      await page.keyboard.press(`${MOD}+k`);
      await page.locator("[cmdk-input]").waitFor();
      await page.keyboard.type("note 1", { delay: 60 });
      await page.waitForTimeout(300);
      for (let i = 0; i < 5; i++) {
        await page.keyboard.press("ArrowDown");
        await page.waitForTimeout(40);
      }
      await page.keyboard.press("Escape");
      await page.locator("[cmdk-input]").waitFor({ state: "detached" });
    },
  },
  {
    id: "e",
    name: "expand/collapse 5 folders, toggle sidebar",
    run: async (page) => {
      for (const p of SMALL_FOLDERS) await folderRow(page, p).click();
      for (const p of [...SMALL_FOLDERS].reverse()) await folderRow(page, p).click();
      await page.keyboard.press(`${MOD}+Backslash`);
      await page.waitForTimeout(150);
      await page.keyboard.press(`${MOD}+Backslash`);
    },
  },
  {
    id: "e-2000",
    name: "expand/collapse 5 folders, toggle sidebar (2000 notes)",
    url: "?vault=large",
    run: async (page) => {
      for (const p of LARGE_FOLDERS) await folderRow(page, p).click();
      for (const p of [...LARGE_FOLDERS].reverse()) await folderRow(page, p).click();
      await page.keyboard.press(`${MOD}+Backslash`);
      await page.waitForTimeout(150);
      await page.keyboard.press(`${MOD}+Backslash`);
    },
  },
  {
    id: "f",
    name: "properties: add tag chip, change status",
    storage: { "nota:properties": "show" },
    run: async (page) => {
      const tags = page
        .locator("dd")
        .filter({ has: page.getByRole("button", { name: "Add" }) })
        .first();
      await tags.getByRole("button", { name: "Add" }).click();
      await page.keyboard.type("perf", { delay: 40 });
      await page.keyboard.press("Enter");
      await page.getByRole("button", { name: "#perf", exact: true }).waitFor();
      await page.getByRole("button", { name: "In progress" }).click();
      await page.getByRole("menuitem", { name: "Done" }).click();
      await page.getByRole("button", { name: "Done" }).waitFor();
    },
  },
  {
    id: "g",
    name: "modes Live/Reading/Source, dark mode, palette",
    run: async (page) => {
      for (const m of ["Reading", "Source", "Live"]) {
        await page.getByRole("button", { name: `${m} mode` }).click();
        await page.waitForTimeout(100);
      }
      await paletteRun(page, "dark mode", "Toggle dark mode");
      await paletteRun(page, "Theme: Graph", "Theme: Graphite");
      await paletteRun(page, "dark mode", "Toggle dark mode");
      await paletteRun(page, "Theme: Nota", "Theme: Nota");
    },
  },
  {
    id: "h",
    name: "idle 5 s after load",
    idle: true,
    run: async (page) => page.waitForTimeout(5000),
  },
  {
    id: "i",
    name: "5000-line note: open, scroll, type",
    url: "?big=1",
    setup: async (page) => expand(page, ["Projects"]),
    run: async (page) => {
      await noteRow(page, "Projects/Big note.md").click();
      await ready(page, "Big note");
      const scroller = page.locator("main .overflow-y-auto").first();
      await scroller.hover();
      for (let i = 0; i < 20; i++) {
        await page.mouse.wheel(0, 1500);
        await page.waitForTimeout(50);
      }
      await page.waitForTimeout(200);
      const box = await scroller.boundingBox();
      await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
      await page.keyboard.press("End");
      await page.keyboard.type(" typing in a very long note!!", { delay: 30 });
    },
    check: (page) => saved(page, "Projects/Big note.md", "typing in a very long note!!"),
  },
];

async function once(sc) {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 1,
  });
  if (args.trace) await context.addInitScript(() => (window.__perfTraceOn = true));
  await context.addInitScript(HOOK);
  if (sc.storage)
    await context.addInitScript((kv) => {
      for (const [k, v] of Object.entries(kv)) localStorage.setItem(k, v);
    }, sc.storage);
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(String(e)));
  const cdp = await context.newCDPSession(page);
  await cdp.send("Performance.enable");
  const metric = async () => {
    const { metrics } = await cdp.send("Performance.getMetrics");
    const get = (n) => metrics.find((x) => x.name === n)?.value ?? 0;
    return { script: get("ScriptDuration") * 1000, task: get("TaskDuration") * 1000 };
  };
  const s = {};
  const m = {};
  let before = { script: 0, task: 0 };
  if (!sc.cold) {
    await page.goto(BASE + (sc.url ?? ""));
    await ready(page);
    if (sc.setup) await sc.setup(page, s);
    await settle(page);
    await page.evaluate(() => window.__perf.reset());
    before = await metric();
  }
  await sc.run(page, m, s);
  if (!sc.idle) await settle(page);
  const after = await metric();
  const st = await page.evaluate(() => window.__perf.read());
  if (sc.check) await sc.check(page);
  if (args.trace)
    for (const c of st.trace) console.error(`  [${sc.id}] t=${c.t} n=${c.n} ${c.top}`);
  await context.close();
  return {
    commits: st.commits,
    renders: st.renders,
    byName: st.byName,
    longTasks: st.longTasks.length,
    longTaskMs: Math.round(st.longTasks.reduce((a, t) => a + t.duration, 0)),
    scriptMs: Math.round(after.script - before.script),
    taskMs: Math.round(after.task - before.task),
    ...m,
    errors,
  };
}

const median = (xs) => {
  const s = [...xs].sort((a, b) => a - b);
  return s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2;
};

const results = {};
for (const sc of SCENARIOS) {
  if (ONLY && !ONLY.has(sc.id)) continue;
  const runs = [];
  for (let i = 0; i < RUNS; i++) {
    const r = await once(sc);
    runs.push(r);
    console.error(
      `${sc.id} run ${i + 1}: commits=${r.commits} renders=${r.renders} long=${r.longTasks}/${r.longTaskMs}ms script=${r.scriptMs}ms${r.readyMs ? ` ready=${r.readyMs}ms` : ""}${r.errors.length ? ` ERRORS=${r.errors.join(" | ")}` : ""}`,
    );
  }
  const names = new Set(runs.flatMap((r) => Object.keys(r.byName)));
  const top = [...names]
    .map((n) => [n, median(runs.map((r) => r.byName[n] ?? 0))])
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .slice(0, 15);
  const med = {};
  for (const k of [
    "commits",
    "renders",
    "longTasks",
    "longTaskMs",
    "scriptMs",
    "taskMs",
    "readyMs",
  ])
    if (runs[0][k] !== undefined) med[k] = median(runs.map((r) => r[k]));
  results[sc.id] = {
    name: sc.name,
    median: med,
    top,
    runs: runs.map(({ byName: _byName, ...r }) => r),
  };
}

let commit = args.commit ?? "";
try {
  if (!commit) commit = execSync("git rev-parse --short HEAD", { cwd: here }).toString().trim();
} catch {}
const out = {
  meta: {
    date: new Date().toISOString(),
    commit,
    runs: RUNS,
    chromium: browser.version(),
    react: "production build (react-dom production bundle), app code unminified, React Compiler on",
    client:
      "in-memory NotesClient (scripts/perf/app/memory-client.ts), desktop-style change events",
    viewport: "1440x900 @1x, headless",
  },
  scenarios: results,
};
await browser.close();
server.close();
const json = JSON.stringify(out, null, 2) + "\n";
if (OUT) writeFileSync(OUT, json);
else process.stdout.write(json);

// --budget perf/budget.json: the render-count regression test. Fails when
// a scenario's median commits or renders go over its budget.
if (args.budget) {
  const budget = JSON.parse(readFileSync(resolve(args.budget), "utf8")).scenarios;
  const over = [];
  for (const [id, r] of Object.entries(results)) {
    const b = budget[id];
    if (!b) continue;
    for (const k of ["commits", "renders"])
      if (r.median[k] > b[k]) over.push(`${id}: ${k} ${r.median[k]} > budget ${b[k]}`);
  }
  if (over.length) {
    console.error(`render budget exceeded:\n  ${over.join("\n  ")}`);
    process.exitCode = 1;
  } else console.error(`render budget: ${Object.keys(results).length} scenarios within budget`);
}
