import type { FolderInfo } from "@nota/client";
import { describe, expect, test } from "vite-plus/test";
import { readFileSync } from "node:fs";
import {
  baseName,
  canAdvance,
  checkName,
  deriveVault,
  initialDraft,
  inboxOf,
  isFullPath,
  joinPath,
  locationError,
  nextStep,
  notesLine,
  prevStep,
  stepsFor,
  type Draft,
} from "../src/onboarding/setup.ts";

// The same cases internal/vault/setup_test.go runs against CheckName.
const cases = JSON.parse(
  readFileSync(new URL("../../../internal/vault/testdata/names.json", import.meta.url), "utf8"),
) as { in: string; kind: "vault" | "inbox"; name: string; error: string }[];

describe("name checks", () => {
  test("give the same answers as the Go checks", () => {
    expect(cases.length).toBeGreaterThan(20);
    for (const c of cases) {
      expect(checkName(c.in, c.kind), JSON.stringify(c.in)).toEqual({
        name: c.name,
        error: c.error,
      });
    }
  });
});

describe("paths", () => {
  test("full paths", () => {
    for (const p of ["/home/me", "~", "~/Documents", "C:\\Users\\me", "c:/x", "\\\\server\\share"])
      expect(isFullPath(p), p).toBe(true);
    for (const p of ["", "docs", "./docs", "~me/x", "Documents/Notes"])
      expect(isFullPath(p), p).toBe(false);
  });

  test("join with the platform's separator", () => {
    expect(joinPath("/home/me/Documents", "Notes", "linux")).toBe("/home/me/Documents/Notes");
    expect(joinPath("/home/me/Documents/", "Notes", "darwin")).toBe("/home/me/Documents/Notes");
    expect(joinPath("C:\\Users\\me", "Notes", "windows")).toBe("C:\\Users\\me\\Notes");
    expect(joinPath("C:\\Users\\me\\", "Notes", "windows")).toBe("C:\\Users\\me\\Notes");
    expect(joinPath("", "Notes", "linux")).toBe("Notes");
  });

  test("base name", () => {
    expect(baseName("/a/b/Writing")).toBe("Writing");
    expect(baseName("/a/b/Writing/")).toBe("Writing");
    expect(baseName("C:\\Users\\me\\Notes")).toBe("Notes");
  });

  test("location errors that need no round trip", () => {
    expect(locationError("")).toBe("Choose where the vault should be created.");
    expect(locationError("docs")).toBe("Use a full path, starting with ~ or /.");
    expect(locationError("/home/me")).toBe("");
  });
});

describe("the draft", () => {
  test("starts with the defaults, or the open vault's inbox when switching", () => {
    expect(initialDraft("/home/me/Documents")).toEqual({
      mode: "create",
      name: "Notes",
      location: "/home/me/Documents",
      existing: "",
      inbox: "Inbox",
      welcome: true,
    });
    expect(initialDraft("/h", "open", "Capture")).toMatchObject({ mode: "open", inbox: "Capture" });
  });
});

describe("step machine", () => {
  test("setup is welcome, vault, inbox, sync, done", () => {
    expect(stepsFor("setup")).toEqual(["welcome", "vault", "inbox", "sync", "done"]);
  });
  test("switching has no welcome and no sign-in", () => {
    expect(stepsFor("switch")).toEqual(["vault", "inbox", "done"]);
  });
  test("next and back stay inside the flow", () => {
    expect(nextStep("setup", "welcome")).toBe("vault");
    expect(nextStep("setup", "inbox")).toBe("sync");
    expect(nextStep("setup", "done")).toBe("done");
    expect(nextStep("switch", "inbox")).toBe("done");
    expect(prevStep("setup", "sync")).toBe("inbox");
    expect(prevStep("setup", "welcome")).toBeNull();
    expect(prevStep("switch", "vault")).toBeNull(); // Esc cancels the overlay
  });
});

const home = "/home/me";
const info = (over: Partial<FolderInfo>): FolderInfo => ({
  path: "",
  exists: false,
  isDir: false,
  empty: false,
  notes: 0,
  files: 0,
  truncated: false,
  writable: true,
  ...over,
});

describe("what the vault step shows", () => {
  const draft = (over: Partial<Draft> = {}): Draft => ({
    ...initialDraft(`${home}/Documents`),
    ...over,
  });

  test("create: the live path, before and after the folder is looked at", () => {
    const d = draft({ name: " Writing " });
    const loading = deriveVault(d, "linux", null);
    expect(loading).toMatchObject({ path: `${home}/Documents/Writing`, ok: false, kind: null });
    expect(loading.target).toBe(`${home}/Documents/Writing`);
    const fresh = deriveVault(d, "linux", info({ path: loading.path }));
    expect(fresh).toMatchObject({ kind: "new", ok: true, canWelcome: true });
  });

  test("create: an empty folder is used as it is", () => {
    const v = deriveVault(draft(), "linux", info({ exists: true, isDir: true, empty: true }));
    expect(v).toMatchObject({ kind: "empty", ok: true, canWelcome: true });
  });

  test("create: a folder with notes is a conflict, with the count, and may not continue", () => {
    const v = deriveVault(
      draft({ name: "Writing" }),
      "linux",
      info({ path: `${home}/Documents/Writing`, exists: true, isDir: true, notes: 124 }),
    );
    expect(v).toMatchObject({ kind: "conflict", ok: false, notes: 124, canWelcome: false });
    // A file where the folder should go is a conflict too.
    expect(deriveVault(draft(), "linux", info({ exists: true, isDir: false })).kind).toBe(
      "conflict",
    );
  });

  test("create: a bad name or location stops before any folder is looked at", () => {
    const bad = deriveVault(draft({ name: "a/b" }), "linux", null);
    expect(bad.nameError).toContain("can’t contain slashes");
    expect(bad.target).toBe("");
    expect(deriveVault(draft({ name: ".x" }), "linux", null).nameError).toContain("hidden");
    expect(deriveVault(draft({ location: "docs" }), "linux", null).locationError).toContain(
      "full path",
    );
    const forbidden = deriveVault(
      draft({ location: "/usr" }),
      "linux",
      null,
      "Nota can’t write to “/usr”. Pick a folder inside your home folder.",
    );
    expect(forbidden.locationError).toContain("Nota can’t write to “/usr”");
    expect(forbidden.ok).toBe(false);
  });

  test("create: a folder Nota can't write to is not ok", () => {
    const v = deriveVault(
      draft(),
      "linux",
      info({ exists: true, isDir: true, empty: true, writable: false }),
    );
    expect(v).toMatchObject({ ok: false, unwritable: true });
  });

  test("open: counts the notes and offers no welcome note for a folder with files", () => {
    const d = draft({ mode: "open", existing: `${home}/Writing` });
    const v = deriveVault(
      d,
      "linux",
      info({ path: d.existing, exists: true, isDir: true, notes: 93, files: 9 }),
    );
    expect(v).toMatchObject({ kind: "existing", ok: true, notes: 93, canWelcome: false });
    expect(notesLine(v)).toBe(
      "Found 93 notes (.md files). Nothing is moved or rewritten; Nota only adds a .nota folder for its index.",
    );
    expect(notesLine({ ...v, notes: 1 })).toContain("Found 1 note (.md files).");
    expect(notesLine({ ...v, notes: 0 })).toContain("No Markdown notes found yet.");
  });

  test("open: an empty folder is a vault with a welcome note on offer", () => {
    const d = draft({ mode: "open", existing: `${home}/Empty` });
    const v = deriveVault(
      d,
      "linux",
      info({ path: d.existing, exists: true, isDir: true, empty: true }),
    );
    expect(v).toMatchObject({ kind: "empty", ok: true, canWelcome: true });
    expect(notesLine(v)).toBe("The folder is empty. Nota will use it as is.");
  });

  test("open: nothing chosen, missing, a file, read-only", () => {
    expect(deriveVault(draft({ mode: "open" }), "linux", null)).toMatchObject({
      ok: false,
      path: "",
    });
    const d = draft({ mode: "open", existing: `${home}/Nope` });
    expect(deriveVault(d, "linux", info({ path: d.existing })).locationError).toContain(
      "doesn’t exist",
    );
    expect(
      deriveVault(d, "linux", info({ path: d.existing, exists: true })).locationError,
    ).toContain("is a file");
    expect(
      deriveVault(
        d,
        "linux",
        info({ path: d.existing, exists: true, isDir: true, notes: 2, writable: false }),
      ),
    ).toMatchObject({ ok: false, unwritable: true });
    expect(
      deriveVault(draft({ mode: "open", existing: "docs" }), "linux", null).locationError,
    ).toContain("full path");
  });

  test("Continue is enabled only when the step is satisfied", () => {
    const d = draft();
    const ok = deriveVault(d, "linux", info({}));
    const conflict = deriveVault(d, "linux", info({ exists: true, isDir: true, notes: 3 }));
    expect(canAdvance("vault", d, ok)).toBe(true);
    expect(canAdvance("vault", d, conflict)).toBe(false);
    expect(canAdvance("inbox", { ...d, inbox: "Capture" }, ok)).toBe(true);
    expect(canAdvance("inbox", { ...d, inbox: "a/b" }, ok)).toBe(false);
    expect(canAdvance("sync", d, ok, true)).toBe(false); // waiting for the browser
    expect(canAdvance("sync", d, ok, false)).toBe(true);
    expect(canAdvance("welcome", d, conflict)).toBe(true);
  });

  test("a blank inbox means the default", () => {
    expect(inboxOf(draft({ inbox: "  " }))).toBe("Inbox");
    expect(inboxOf(draft({ inbox: " Capture " }))).toBe("Capture");
  });
});
