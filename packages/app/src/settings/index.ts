import { useSyncExternalStore } from "react";
import { setThemeHooks, themeStore } from "../theme/store.ts";
import type { SettingName } from "./keys.ts";
import { createSettingsSync, type SettingSource } from "./sync.ts";

export type { SettingName } from "./keys.ts";
export type { SettingSource } from "./sync.ts";
export { SETTING_KEYS } from "./keys.ts";

function deviceStorage() {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

/** The app's settings layers (one per window). */
export const settingsSync = createSettingsSync(deviceStorage());

/** Whether settings.toml sets the key, and where the value in effect comes from. */
export function useSettingSource(name: SettingName): { locked: boolean; source: SettingSource } {
  const source = useSyncExternalStore(
    settingsSync.subscribe,
    () => settingsSync.source(name),
    () => settingsSync.source(name),
  );
  return { locked: source === "file", source };
}

/** The layers as last loaded (path, warnings, signed in), live. */
export function useSettingsLayers() {
  return useSyncExternalStore(settingsSync.subscribe, settingsSync.layers, settingsSync.layers);
}

// The theme store reads and writes localStorage; the layers lock its keys,
// hear its changes and tell it to read again.
setThemeHooks({
  locked: (k) => settingsSync.locked(k),
  onChange: (k, v) => settingsSync.changed(k, v),
});
settingsSync.onReload(() => themeStore().reload());
