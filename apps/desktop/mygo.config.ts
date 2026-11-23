import { defineConfig } from "mygo-cli";

// The release workflow sets NOTA_VERSION from the tag (v1.2.3 -> 1.2.3);
// local builds keep the fallback. (No @types/node here, hence globalThis.)
const env: Record<string, string | undefined> =
  (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env ?? {};
const version = env.NOTA_VERSION || "0.0.1";

export default defineConfig({
  name: "Nota",
  slug: "nota",
  identifier: "com.banjoanton.nota",
  version,
  copyright: "© 2026 Anton Ödman",
  devUrl: "http://localhost:5173",
  devCommand: "vp dev",
  buildCommand: "vp build",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
  // Sign-in to sync comes back as nota://auth?code=… (account.go).
  urlSchemes: ["nota"],
  macos: {
    // Ad hoc: there is no Developer ID yet, so the app is not notarized.
    signingIdentity: "-",
    infoPlist: { LSApplicationCategoryType: "public.app-category.productivity" },
  },
  linux: {
    maintainer: "Anton Ödman <anton.odman@gmail.com>",
    comment: "Markdown notes as plain files",
    categories: ["Office", "Utility"],
    formats: ["deb", "rpm"],
  },
});
