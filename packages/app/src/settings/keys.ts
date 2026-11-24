import { NOTE_MODE_KEY, readNoteMode } from "../lib/noteMode.ts";
import { PROPERTIES_KEY, readPropertiesPref } from "../lib/propertiesPref.ts";
import { PALETTE_KEY, readMode, readPalette, THEME_KEY } from "../theme/store.ts";

// The user's settings (internal/settings has the same keys; a test
// compares them with the schema it generates). Each one is kept on the
// device under its own localStorage key, where the default is stored as
// "nothing", as before the settings had layers.

export interface SettingKey {
  /** The name in settings.toml and the account, e.g. "note_mode". */
  name: string;
  /** Its localStorage key on the device. */
  storage: string;
  default: string;
  /** The value, or the default for anything unknown. */
  read: (v: string | null | undefined) => string;
}

export const SETTING_KEYS: readonly SettingKey[] = [
  { name: "theme", storage: THEME_KEY, default: "system", read: readMode },
  { name: "palette", storage: PALETTE_KEY, default: "nota", read: readPalette },
  { name: "note_mode", storage: NOTE_MODE_KEY, default: "live", read: readNoteMode },
  { name: "properties", storage: PROPERTIES_KEY, default: "hide", read: readPropertiesPref },
];

export type SettingName = "theme" | "palette" | "note_mode" | "properties";

/** Whether v is a value the key takes (not just something read() turns into one). */
export const isValid = (k: SettingKey, v: string | null | undefined): v is string =>
  typeof v === "string" && k.read(v) === v;
