// Whether a note's properties (its frontmatter) show above the body.

import {
  DEFAULT_SHOW_PREF,
  readShowPref,
  showPref,
  type ShowPref,
  toggledShowPref,
} from "./showPref.ts";

export type PropertiesPref = ShowPref;

const pref = showPref("nota:properties");

export const PROPERTIES_KEY = pref.key;
export const DEFAULT_PROPERTIES: PropertiesPref = DEFAULT_SHOW_PREF;
export const readPropertiesPref = readShowPref;
export const loadPropertiesPref = pref.load;
export const savePropertiesPref = pref.save;
export const toggledPropertiesPref = toggledShowPref;
export const watchPropertiesPref = pref.watch;

/** How many properties the chip counts: every key in the note's frontmatter. */
export const propertyCount = (props: Record<string, unknown> | undefined) =>
  Object.keys(props ?? {}).length;
