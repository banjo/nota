import { describe, expect, test } from "vite-plus/test";
import { diffLines, diffStats } from "../src/lib/diff.ts";

const render = (a: string, b: string) =>
  diffLines(a, b)
    .map((l) => (l.kind === "add" ? "+" : l.kind === "del" ? "-" : " ") + l.text)
    .join("\n");

/** Applying the diff gives back both sides. */
function check(a: string, b: string) {
  const d = diffLines(a, b);
  const side = (keep: "add" | "del") =>
    d
      .filter((l) => l.kind !== keep)
      .map((l) => `${l.text}\n`)
      .join("");
  expect(side("add")).toBe(a);
  expect(side("del")).toBe(b);
}

describe("diffLines", () => {
  test("marks what was added and removed", () => {
    expect(render("# Plan\nA\nB\nC\n", "# Plan\nA\nX\nC\nD\n")).toBe(" # Plan\n A\n-B\n+X\n C\n+D");
  });

  test("handles empty sides and equal texts", () => {
    expect(render("", "a\nb\n")).toBe("+a\n+b");
    expect(render("a\n", "")).toBe("-a");
    expect(diffStats(diffLines("a\nb\n", "a\nb\n"))).toEqual({ added: 0, removed: 0 });
  });

  test("is minimal and reversible on mixed edits", () => {
    const cases: [string, string][] = [
      ["a\nb\nc\nd\ne\n", "x\nb\nc\ny\ne\nz\n"],
      ["1\n2\n3\n", "3\n2\n1\n"],
      ["same\n".repeat(50) + "end\n", "start\n" + "same\n".repeat(50)],
      ["a\nb\na\nb\n", "b\na\nb\na\n"],
    ];
    for (const [a, b] of cases) check(a, b);
    expect(diffStats(diffLines("a\nb\nc\nd\ne\n", "x\nb\nc\ny\ne\nz\n"))).toEqual({
      added: 3,
      removed: 2,
    });
  });
});
