import { defineConfig } from "astro/config";

// The landing page is the site root; the web app is built on top of it
// at /app/ (apps/web). `bunx vp run site:build` builds this first into
// ../../dist (Astro empties it), then the app into ../../dist/app.
//
// Every page is prerendered HTML. The page ships no framework: Astro
// components render at build time, and the only JavaScript is one small
// vanilla module (theme, copy buttons, tabs, the hero animation).
export default defineConfig({
  site: "https://nota.banjoanton.com",
  outDir: "../../dist",
  trailingSlash: "ignore",
  devToolbar: { enabled: false },
  // One request fewer: the page's CSS is small enough to inline.
  build: { inlineStylesheets: "always" },
  server: { port: 5175 },
});
