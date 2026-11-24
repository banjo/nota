import type { SettingsApi, SettingsLayers, SettingValues } from "@nota/client";
import { isValid, SETTING_KEYS, type SettingKey, type SettingName } from "./keys.ts";

// The user's settings come in layers; the highest one that sets a key
// wins: settings.toml (locked: the app never changes it), the account
// (what the app writes while signed in), the device (localStorage), the
// default. The stores (theme, note mode, properties) keep reading and
// writing localStorage as they always did: this keeps it holding the
// merged values, so a window's first paint (the inline script in
// index.html) is right before anything has loaded, and sends the
// user's changes to the account.

type Storage = Pick<globalThis.Storage, "getItem" | "setItem" | "removeItem">;

export type SettingSource = "file" | "account" | "device" | "default";

export interface SettingsSync {
  /** The layers as last loaded; null before the first load, or without an api. */
  layers: () => SettingsLayers | null;
  /** settings.toml sets the key: the app shows it and does not change it. */
  locked: (name: SettingName) => boolean;
  /** Which layer the value in effect comes from. */
  source: (name: SettingName) => SettingSource;
  /** Every key with the value in effect (what the stores show). */
  values: () => SettingValues;
  /**
   * The user changed a setting (already in localStorage): send it to the
   * account when signed in. Locked keys are not sent.
   */
  changed: (name: SettingName, value: string) => void;
  /** Runs fn after localStorage was brought in line with the layers. */
  onReload: (fn: () => void) => () => void;
  /** For React: called whenever layers or sources change. */
  subscribe: (fn: () => void) => () => void;
  /** Loads the layers and follows them; returns the stop function. */
  connect: (api: SettingsApi | undefined) => () => void;
}

const byName = (name: string): SettingKey | undefined => SETTING_KEYS.find((k) => k.name === name);

function read(storage: Storage | null, key: string): string | null {
  try {
    return storage?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function write(storage: Storage | null, k: SettingKey, value: string) {
  try {
    if (value === k.default) storage?.removeItem(k.storage);
    else storage?.setItem(k.storage, value);
  } catch {
    // Private browsing: the value lasts this page.
  }
}

export function createSettingsSync(storage: Storage | null): SettingsSync {
  let layers: SettingsLayers | null = null;
  let api: SettingsApi | undefined;
  // Changes sent to the account and not answered yet: they win over what
  // the account said before them.
  const sent = new Map<string, string>();
  const reloaders = new Set<() => void>();
  const listeners = new Set<() => void>();
  const notify = () => {
    for (const fn of listeners) fn();
  };

  /** The value the file or the account gives a key, if a valid one. */
  const above = (k: SettingKey, l: SettingsLayers): string | undefined => {
    if (isValid(k, l.file[k.name])) return l.file[k.name];
    if (sent.has(k.name)) return sent.get(k.name);
    const a = l.account?.[k.name];
    return isValid(k, a) ? a : undefined;
  };

  /** Brings localStorage in line with the layers and tells the stores. */
  const apply = (next: SettingsLayers) => {
    layers = next;
    let moved = false;
    for (const k of SETTING_KEYS) {
      const v = above(k, next);
      if (v === undefined) continue;
      if (k.read(read(storage, k.storage)) !== v) {
        write(storage, k, v);
        moved = true;
      }
    }
    if (moved) for (const fn of reloaders) fn();
    notify();
  };

  /**
   * A first sign-in takes this device's choices to the account, once:
   * when the account has none yet. Otherwise the account wins.
   */
  const adopt = async (l: SettingsLayers): Promise<SettingsLayers> => {
    if (!api || !l.signedIn || !l.account || Object.keys(l.account).length > 0) return l;
    const set: SettingValues = {};
    for (const k of SETTING_KEYS) {
      const v = read(storage, k.storage);
      if (isValid(k, v) && !isValid(k, l.file[k.name])) set[k.name] = v;
    }
    if (Object.keys(set).length === 0) return l;
    try {
      return await api.update({ set });
    } catch {
      return l;
    }
  };

  const load = async (from: SettingsApi) => {
    try {
      const l = await adopt(await from.get());
      if (api === from) apply(l);
    } catch {
      // The device's values stay; the next event tries again.
    }
  };

  return {
    layers: () => layers,
    locked: (name) => {
      const k = byName(name);
      return !!k && !!layers && isValid(k, layers.file[name]);
    },
    source: (name) => {
      const k = byName(name);
      if (!k) return "default";
      if (layers && isValid(k, layers.file[name])) return "file";
      if (layers && (sent.has(name) || isValid(k, layers.account?.[name]))) return "account";
      return read(storage, k.storage) === null ? "default" : "device";
    },
    values() {
      const out: SettingValues = {};
      for (const k of SETTING_KEYS) {
        const f = layers?.file[k.name];
        out[k.name] = isValid(k, f) ? f : k.read(read(storage, k.storage));
      }
      return out;
    },
    changed(name, value) {
      const k = byName(name);
      if (!k || !api || !layers?.signedIn || isValid(k, layers.file[name])) {
        notify();
        return;
      }
      sent.set(name, value);
      notify();
      const from = api;
      from.update({ set: { [name]: value } }).then(
        (l) => {
          if (sent.get(name) === value) sent.delete(name);
          if (api === from) apply(l);
        },
        () => {
          // Offline: the choice stays on this device; the account's value
          // comes back when the account is reachable again.
          if (sent.get(name) === value) sent.delete(name);
          notify();
        },
      );
    },
    onReload: (fn) => {
      reloaders.add(fn);
      return () => reloaders.delete(fn);
    },
    subscribe: (fn) => {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
    connect(next) {
      api = next;
      if (!next) return () => {};
      void load(next);
      const off = next.subscribe((l) => {
        if (api === next) void adopt(l).then((a) => api === next && apply(a));
      });
      return () => {
        off();
        if (api === next) {
          api = undefined;
          layers = null;
          notify();
        }
      };
    },
  };
}
