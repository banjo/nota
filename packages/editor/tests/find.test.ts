import { SearchQuery } from "@codemirror/search";
import { EditorSelection, EditorState } from "@codemirror/state";
import { describe, expect, test } from "vite-plus/test";
import { matchPosition, presetMatch } from "../src/find.ts";

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

describe("opening the bar on a match from elsewhere", () => {
  const doc = "Plan A\n\nVi gick igenom planen, och planen gäller.\n";
  const pick = (near?: { before: string; text: string }) =>
    presetMatch(state(doc), { search: "planen", near }).range;

  test("selects the match after the text before it on its line", () => {
    expect(pick({ before: "…igenom planen, och ", text: "planen" })).toMatchObject({
      from: 35,
      to: 41,
    });
  });

  test("falls back to the first match", () => {
    expect(pick({ before: "not there ", text: "planen" })).toMatchObject({ from: 23, to: 29 });
    expect(pick()).toMatchObject({ from: 23, to: 29 });
    expect(presetMatch(state(doc), { search: "(", regexp: true }).range).toBeUndefined();
  });
});
