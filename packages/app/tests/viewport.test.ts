import { describe, expect, it } from "vite-plus/test";
import { keyboardInset } from "../src/lib/viewport.ts";

describe("keyboardInset", () => {
  const base = { innerHeight: 844, height: 844, offsetTop: 0, scale: 1 };

  it("is 0 with nothing covering the page", () => {
    expect(keyboardInset(base)).toBe(0);
  });

  it("measures a keyboard from the visual viewport", () => {
    expect(keyboardInset({ ...base, height: 508 })).toBe(336);
  });

  it("subtracts the page iOS panned up to show the caret", () => {
    expect(keyboardInset({ ...base, height: 508, offsetTop: 40 })).toBe(296);
  });

  it("ignores browser chrome moving a little", () => {
    expect(keyboardInset({ ...base, height: 790 })).toBe(0);
  });

  it("ignores pinch zoom, which shrinks the visual viewport too", () => {
    expect(keyboardInset({ ...base, height: 422, scale: 2 })).toBe(0);
  });
});
