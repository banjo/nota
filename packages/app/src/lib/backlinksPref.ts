// Whether "Linked from" (the notes that link here) shows under a note.

import { showPref } from "./showPref.ts";

export const backlinksPref = showPref("nota:backlinks");

/** The notes that link to `path`, once each, without the note itself. */
export const backlinkSources = (links: { from: string }[], path: string) =>
  [...new Set(links.map((l) => l.from))].filter((p) => p !== path);
