import { readFileSync } from "node:fs";
import type { SettingsApi, SettingsLayers, SettingsPatch } from "@nota/client";
import { describe, expect, test, vi } from "vite-plus/test";
import { SETTING_KEYS } from "../src/settings/keys.ts";
import { createSettingsSync } from "../src/settings/sync.ts";
import { SCHEMA_URL, settingsToml } from "../src/settings/toml.ts";

function fakeStorage(stored: Record<string, string> = {}) {
  const data = new Map(Object.entries(stored));
  return {
    data,
    storage: {
      getItem: (k: string) => data.get(k) ?? null,
      setItem: (k: string, v: string) => void data.set(k, v),
      removeItem: (k: string) => void data.delete(k),
    },
  };
}

const base: SettingsLayers = { file: {}, account: null, signedIn: false, path: "", warnings: [] };

/** A settings api over one account, whose events the test fires. */
function fakeApi(layers: Partial<SettingsLayers>) {
  let current: SettingsLayers = { ...base, ...layers };
  let listener: (l: SettingsLayers) => void = () => {};
  const update = vi.fn(async (p: SettingsPatch) => {
    const account = { ...current.account };
    Object.assign(account, p.set);
    for (const k of p.unset ?? []) delete account[k];
    current = { ...current, account };
    return current;
  });
  const api: SettingsApi = {
    get: vi.fn(async () => current),
    update,
    subscribe: (l) => {
      listener = l;
      return () => {};
    },
  };
  return {
    api,
    update,
    /** A layer changed somewhere else. */
    push(next: Partial<SettingsLayers>) {
      current = { ...current, ...next };
      listener(current);
    },
  };
}

const flush = () => new Promise((r) => setTimeout(r, 0));

describe("the settings layers", () => {
  test("bring the file's and the account's values into localStorage", async () => {
    const { data, storage } = fakeStorage({ "nota:theme": "light", "nota:note-mode": "source" });
    const s = createSettingsSync(storage);
    const reload = vi.fn();
    s.onReload(reload);
    const f = fakeApi({
      file: { palette: "paper", properties: "nonsense" },
      account: { theme: "dark", palette: "forest" },
      signedIn: true,
    });
    s.connect(f.api);
    await flush();
    expect(Object.fromEntries(data)).toEqual({
      "nota:theme": "dark",
      "nota:palette": "paper",
      "nota:note-mode": "source",
    });
    expect(reload).toHaveBeenCalledTimes(1);
    expect(s.locked("palette")).toBe(true);
    expect(s.locked("properties")).toBe(false); // not a valid value: ignored
    expect(s.source("theme")).toBe("account");
    expect(s.source("palette")).toBe("file");
    expect(s.source("note_mode")).toBe("device");
    expect(s.source("properties")).toBe("default");
    expect(s.values()).toEqual({
      theme: "dark",
      palette: "paper",
      note_mode: "source",
      properties: "hide",
      backlinks: "hide",
    });

    // The account's value going back to the default clears the device's.
    f.push({ account: { theme: "system" } });
    await flush();
    expect(data.has("nota:theme")).toBe(false);
    expect(reload).toHaveBeenCalledTimes(2);
    // Nothing moved: the stores are left alone.
    f.push({ warnings: ["x"] });
    await flush();
    expect(reload).toHaveBeenCalledTimes(2);
    expect(s.layers()?.warnings).toEqual(["x"]);
  });

  test("send the user's changes to the account when signed in", async () => {
    const { storage } = fakeStorage();
    const s = createSettingsSync(storage);
    const f = fakeApi({ file: { palette: "paper" }, account: { theme: "dark" }, signedIn: true });
    s.connect(f.api);
    await flush();
    s.changed("note_mode", "reading");
    expect(s.source("note_mode")).toBe("account");
    await flush();
    expect(f.update).toHaveBeenCalledWith({ set: { note_mode: "reading" } });
    // A key the file sets never goes to the account.
    s.changed("palette", "forest");
    expect(f.update).toHaveBeenCalledTimes(1);
  });

  test("keep changes on the device when signed out", async () => {
    const { storage } = fakeStorage();
    const s = createSettingsSync(storage);
    const f = fakeApi({ signedIn: false });
    s.connect(f.api);
    await flush();
    s.changed("theme", "dark");
    expect(f.update).not.toHaveBeenCalled();
  });

  test("a change on its way wins over what the account said before it", async () => {
    const { data, storage } = fakeStorage({ "nota:theme": "dark" });
    const s = createSettingsSync(storage);
    const f = fakeApi({ account: { theme: "dark" }, signedIn: true });
    let answer: (l: SettingsLayers) => void = () => {};
    f.api.update = vi.fn(() => new Promise<SettingsLayers>((r) => (answer = r)));
    s.connect(f.api);
    await flush();
    data.set("nota:theme", "light"); // the store wrote it
    s.changed("theme", "light");
    // An event from before the change arrives first.
    f.push({ account: { theme: "dark" } });
    await flush();
    expect(data.get("nota:theme")).toBe("light");
    answer({ ...base, signedIn: true, account: { theme: "light" } });
    await flush();
    expect(data.get("nota:theme")).toBe("light");
    expect(s.source("theme")).toBe("account");
  });

  test("a first sign-in takes this device's choices to an empty account", async () => {
    const { storage } = fakeStorage({ "nota:theme": "dark", "nota:palette": "forest" });
    const s = createSettingsSync(storage);
    const f = fakeApi({ file: { palette: "paper" }, account: {}, signedIn: true });
    s.connect(f.api);
    await flush();
    // The file's key stays out of it.
    expect(f.update).toHaveBeenCalledWith({ set: { theme: "dark" } });
    expect(s.source("theme")).toBe("account");

    // An account with settings wins instead.
    const other = fakeStorage({ "nota:theme": "light" });
    const s2 = createSettingsSync(other.storage);
    const f2 = fakeApi({ account: { theme: "dark" }, signedIn: true });
    s2.connect(f2.api);
    await flush();
    expect(f2.update).not.toHaveBeenCalled();
    expect(other.data.get("nota:theme")).toBe("dark");
  });

  test("disconnecting forgets the layers", async () => {
    const s = createSettingsSync(fakeStorage().storage);
    const off = s.connect(fakeApi({ file: { theme: "dark" } }).api);
    await flush();
    expect(s.locked("theme")).toBe(true);
    off();
    expect(s.locked("theme")).toBe(false);
    expect(s.layers()).toBeNull();
  });
});

describe("settings.toml", () => {
  test("has the keys of the schema internal/settings generates", () => {
    const schema = JSON.parse(
      readFileSync(
        new URL("../../../apps/landing/public/settings.schema.json", import.meta.url),
        "utf8",
      ),
    ) as { $id: string; properties: Record<string, { default: string }> };
    expect(SETTING_KEYS.map((k) => k.name).sort()).toEqual(Object.keys(schema.properties).sort());
    for (const k of SETTING_KEYS) expect(k.default).toBe(schema.properties[k.name]!.default);
    expect(SCHEMA_URL).toBe(schema.$id);
  });

  test("copies as TOML", () => {
    expect(settingsToml({ properties: "show", theme: "dark", palette: 'a"b' })).toBe(
      `#:schema ${SCHEMA_URL}\ntheme = "dark"\npalette = "a\\"b"\nproperties = "show"\n`,
    );
  });
});
