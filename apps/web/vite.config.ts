import babel from "@rolldown/plugin-babel";
import tailwindcss from "@tailwindcss/vite";
import react, { reactCompilerPreset } from "@vitejs/plugin-react";
import { defineConfig } from "vite-plus";

// The web app is built into the landing site's dist as /app/ (one
// Cloudflare deploy serves both, docs/deploy.md). `bunx vp run site:build`
// builds the landing first, then this.
export default defineConfig({
  base: "/app/",
  plugins: [react(), babel({ presets: [reactCompilerPreset()] }), tailwindcss()],
  build: {
    outDir: "../../dist/app",
    emptyOutDir: true,
    target: "es2022",
  },
  server: {
    port: 5174,
    strictPort: true,
  },
});
