import { SearchQuery } from "@codemirror/search";
import { EditorSelection, EditorState } from "@codemirror/state";
import { describe, expect, test } from "vite-plus/test";
import { matchPosition } from "../src/find.ts";

const state = (doc: string, from = 0, to = from) =>
  EditorState.create({ doc, selection: EditorSelection.single(from, to) });

describe("the find bar's count", () => {
  test("says which match is selected, out of how many", () => {
    const q = new SearchQuery({ search: "plan" });
    expect(matchPosition(state("Plan, plan och planen", 6, 10), q)).toEqual({
      index: 1,
      total: 3,
      more: false,
    });
    // No match selected: only the total.
    expect(matchPosition(state("Plan, plan och planen", 0, 0), q).index).toBe(-1);
  });

  test("follows case, whole word and regex", () => {
    const doc = "Plan, plan och planen";
    expect(
      matchPosition(state(doc), new SearchQuery({ search: "plan", caseSensitive: true })).total,
    ).toBe(2);
    expect(
      matchPosition(state(doc), new SearchQuery({ search: "plan", wholeWord: true })).total,
    ).toBe(2);
    expect(
      matchPosition(state(doc), new SearchQuery({ search: "plan(en)?$", regexp: true })).total,
    ).toBe(1);
  });

  test("an invalid regex or an empty query counts nothing", () => {
    expect(matchPosition(state("a(b"), new SearchQuery({ search: "(", regexp: true })).total).toBe(
      0,
    );
    expect(matchPosition(state("abc"), new SearchQuery({ search: "" })).total).toBe(0);
  });

  test("stops counting at 1000", () => {
    const r = matchPosition(state("x".repeat(1500)), new SearchQuery({ search: "x" }));
    expect(r).toEqual({ index: -1, total: 1000, more: true });
  });
});
