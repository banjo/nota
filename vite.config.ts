import { defineConfig } from "vite-plus";

export default defineConfig({
  fmt: {
    // Generated: mygo.ts by `mygo generate`, the schema by internal/settings.
    ignorePatterns: ["**/src/mygo.ts", "docs/**", "apps/landing/public/settings.schema.json"],
  },
  lint: {
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    rules: { "vite-plus/prefer-vite-plus-imports": "error" },
    options: { typeAware: true, typeCheck: true },
    ignorePatterns: ["**/src/mygo.ts", "**/build/**", "**/.mygo/**"],
  },
  run: {
    cache: true,
  },
});
