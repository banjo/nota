import type { NoteInfo } from "@nota/client";
import { describe, expect, test } from "vite-plus/test";
import { movePin, nextPin, pinnedNotes, pinOrder } from "../src/lib/pinned.ts";

const note = (path: string, pinned?: unknown): NoteInfo => ({
  path,
  folder: "",
  title: path,
  tags: [],
  properties: pinned === undefined ? {} : { pinned },
  modified: "",
  size: 0,
});

describe("pinned notes", () => {
  test("a number is the place, true comes last, anything else does not pin", () => {
    expect(pinOrder(note("a", 20))).toBe(20);
    expect(pinOrder(note("a", true))).toBe(Infinity);
    for (const v of [false, "yes", 0 / 0, undefined])
      expect(pinOrder(note("a", v))).toBeUndefined();
    const list = pinnedNotes([
      note("t", true),
      note("b", 100),
      note("x"),
      note("a", 20),
      note("f", false),
    ]);
    expect(list.map((n) => n.path)).toEqual(["a", "b", "t"]);
  });

  test("a new pin goes ten past the highest number", () => {
    expect(nextPin([])).toBe(10);
    expect(nextPin([note("a", 20), note("t", true), note("b", 15.5)])).toBe(30);
  });

  const list = [note("a", 10), note("b", 20), note("c", 30)];

  test("a move writes one note when there is room between the neighbours", () => {
    expect(movePin(list, "c", 0)).toEqual([{ path: "c", pinned: 5 }]);
    expect(movePin(list, "a", 1)).toEqual([{ path: "a", pinned: 25 }]);
    expect(movePin(list, "a", 2)).toEqual([{ path: "a", pinned: 40 }]);
    expect(movePin(list, "b", 1)).toEqual([]);
  });

  test("without room the list is renumbered, writing only what changed", () => {
    const tight = [note("a", 1), note("b", 2), note("c", 30)];
    expect(movePin(tight, "c", 1)).toEqual([
      { path: "a", pinned: 10 },
      { path: "c", pinned: 20 },
      { path: "b", pinned: 30 },
    ]);
    // Notes pinned with true get numbers when a note moves below them.
    expect(movePin([note("a", 10), note("t", true)], "a", 1)).toEqual([
      { path: "t", pinned: 10 },
      { path: "a", pinned: 20 },
    ]);
  });
});
