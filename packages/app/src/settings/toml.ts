import type { SettingValues } from "@nota/client";
import { SETTING_KEYS } from "./keys.ts";

/** Where the landing site serves the schema (internal/settings.SchemaURL). */
export const SCHEMA_URL = "https://nota.banjoanton.com/settings.schema.json";

/**
 * Settings as settings.toml content, in the order of the keys, under the
 * #:schema line editors complete from. Values are ids and words, so a
 * basic string needs no escaping beyond quotes and backslashes.
 */
export function settingsToml(values: SettingValues): string {
  const lines = [`#:schema ${SCHEMA_URL}`];
  for (const k of SETTING_KEYS) {
    const v = values[k.name];
    if (v === undefined) continue;
    lines.push(`${k.name} = "${v.replace(/[\\"]/g, (c) => `\\${c}`)}"`);
  }
  return lines.join("\n") + "\n";
}
