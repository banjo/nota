import { defineConfig } from "vite-plus";

// The landing page is the site root; the web app is built on top of it
// at /app/ (see apps/web/vite.config.ts). `bunx vp run site:build`
// builds this first into ../../dist, then the app into ../../dist/app.
export default defineConfig({
  build: {
    outDir: "../../dist",
    emptyOutDir: true,
    target: "es2022",
  },
  server: {
    port: 5175,
    strictPort: true,
    // Working on the landing page against the running web app.
    proxy: {
      "/app": { target: "http://localhost:5174" },
    },
  },
});
