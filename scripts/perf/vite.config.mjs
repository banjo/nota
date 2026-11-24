// Builds the perf harness (scripts/perf/app) the way apps/web is built:
// React + the React Compiler + Tailwind, production React. App code is left
// unminified so component names survive for the render counts.
//
//   bunx vp build --config scripts/perf/vite.config.mjs
//
// scripts/perf is not a workspace package (bun's isolated installs hoist
// nothing to the root), so plugins and React resolve through apps/web.
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { defineConfig } from "vite-plus";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "../..");
const web = createRequire(resolve(repo, "apps/web/package.json"));
const load = async (id) => import(pathToFileURL(web.resolve(id)).href);
const pkgDir = (id) => dirname(web.resolve(`${id}/package.json`));

const { default: react, reactCompilerPreset } = await load("@vitejs/plugin-react");
const { default: babel } = await load("@rolldown/plugin-babel");
const { default: tailwindcss } = await load("@tailwindcss/vite");

export default defineConfig({
  root: resolve(here, "app"),
  base: "./",
  mode: "production",
  plugins: [react(), babel({ presets: [reactCompilerPreset()] }), tailwindcss()],
  resolve: {
    alias: [
      { find: /^@nota\/app$/, replacement: resolve(repo, "packages/app/src/index.ts") },
      { find: /^@nota\/client$/, replacement: resolve(repo, "packages/client/src/index.ts") },
      {
        find: /^@nota\/ui\/styles\.css$/,
        replacement: resolve(repo, "packages/ui/src/styles.css"),
      },
      { find: "react-dom", replacement: pkgDir("react-dom") },
      { find: "react", replacement: pkgDir("react") },
    ],
  },
  build: {
    outDir: resolve(here, "dist"),
    emptyOutDir: true,
    target: "es2022",
    minify: false,
    sourcemap: false,
  },
});
