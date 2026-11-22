import { EditorState } from "@codemirror/state";
import { expect, test } from "vite-plus/test";
import { extensions } from "../src/index.ts";

test("markdown extensions load into an editor state", () => {
  const state = EditorState.create({ doc: "# Hej\n\n- [ ] uppgift", extensions });
  expect(state.doc.lines).toBe(3);
});
