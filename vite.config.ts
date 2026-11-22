import { defineConfig } from "vite-plus";

export default defineConfig({
  fmt: {
    ignorePatterns: ["prototypes/**", "**/src/mygo.ts", "docs/**"],
  },
  lint: {
    jsPlugins: [{ name: "vite-plus", specifier: "vite-plus/oxlint-plugin" }],
    rules: { "vite-plus/prefer-vite-plus-imports": "error" },
    options: { typeAware: true, typeCheck: true },
    ignorePatterns: ["prototypes/**", "**/src/mygo.ts", "**/build/**", "**/.mygo/**"],
  },
  run: {
    cache: true,
  },
});
