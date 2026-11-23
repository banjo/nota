// Renders og.html to apps/landing/public/og.png (1200x630) with
// Playwright's Chromium. Playwright is not a dependency of the repo, so
// run a copy next to an install of it, with the repo root as argument:
//   cp assets/brand/render-og.mjs /tmp/pw/ && node /tmp/pw/render-og.mjs "$PWD"
import { chromium } from "playwright";
import { pathToFileURL } from "node:url";
import { join } from "node:path";

const root = process.argv[2] ?? process.cwd();
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1200, height: 630 } });
await page.goto(pathToFileURL(join(root, "assets/brand/og.html")).href, {
  waitUntil: "networkidle",
});
await page.evaluate(() => document.fonts.ready);
await page.screenshot({ path: join(root, "apps/landing/public/og.png") });
await browser.close();
