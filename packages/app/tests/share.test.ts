import { describe, expect, test } from "vite-plus/test";
import { sameTheme } from "../src/lib/share.ts";

describe("sameTheme", () => {
  test("compares mode and tokens", () => {
    const a = { mode: "dark", tokens: { ink: "#fff", background: "#000" } };
    expect(sameTheme(a, { mode: "dark", tokens: { background: "#000", ink: "#fff" } })).toBe(true);
    expect(sameTheme(a, { mode: "light", tokens: a.tokens })).toBe(false);
    expect(sameTheme(a, { mode: "dark", tokens: { ink: "#fff" } })).toBe(false);
    expect(sameTheme(undefined, {})).toBe(true);
  });
});
