import { defineConfig } from "mygo-cli";

export default defineConfig({
  name: "Nota",
  identifier: "com.banjoanton.nota",
  version: "0.0.1",
  devUrl: "http://localhost:5173",
  devCommand: "vp dev",
  buildCommand: "vp build",
  frontendDist: "dist",
  bindings: "src/mygo.ts",
  out: "build",
  // Sign-in to sync comes back as nota://auth?code=… (account.go).
  urlSchemes: ["nota"],
});
