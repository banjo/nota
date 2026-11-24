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
  // Markdown files open with the app (open.go): a note of the vault as
  // that note, any other file as a preview. This list decides which
  // command-line arguments reach App.OnOpenFile and the Linux desktop
  // entry's MimeType; macOS gets its own entry below.
  fileAssociations: [
    {
      ext: ["md", "markdown"],
      name: "Markdown Document",
      mimeType: "text/markdown",
      role: "Viewer",
    },
  ],
  macos: {
    // Ad hoc: there is no Developer ID yet, so the app is not notarized.
    signingIdentity: "-",
    infoPlist: {
      LSApplicationCategoryType: "public.app-category.productivity",
      // Replaces the entry MyGo makes from fileAssociations, which has
      // LSHandlerRank "Default" (cmd/mygo/bundle.go in v0.10.0): Nota is
      // offered in Open With but never made the default app for .md.
      // Keep the extensions in step with fileAssociations above; the
      // release workflow checks the built Info.plist.
      CFBundleDocumentTypes: [
        {
          CFBundleTypeName: "Markdown Document",
          CFBundleTypeRole: "Viewer",
          LSHandlerRank: "Alternate",
          LSItemContentTypes: ["net.daringfireball.markdown"],
          CFBundleTypeExtensions: ["md", "markdown"],
          CFBundleTypeMIMETypes: ["text/markdown"],
        },
      ],
      // For macOS versions that do not declare the Markdown type.
      UTImportedTypeDeclarations: [
        {
          UTTypeIdentifier: "net.daringfireball.markdown",
          UTTypeDescription: "Markdown Document",
          UTTypeConformsTo: ["public.plain-text"],
          UTTypeTagSpecification: {
            "public.filename-extension": ["md", "markdown"],
            "public.mime-type": ["text/markdown"],
          },
        },
      ],
    },
  },
  linux: {
    maintainer: "Anton Ödman <anton.odman@gmail.com>",
    comment: "Markdown notes as plain files",
    categories: ["Office", "Utility"],
    formats: ["deb", "rpm"],
  },
});
