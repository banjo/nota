// The page's only script: the dark-mode toggle, copy buttons, the tabs of
// the agent box and the hero animation. Everything it touches is already
// in the prerendered HTML; without it the page reads the same.

import { initHero } from "./hero.ts";

const THEME_KEY = "nota:theme";

function initTheme() {
  const root = document.documentElement;
  const system = matchMedia("(prefers-color-scheme: dark)");
  const stored = () => {
    try {
      const t = localStorage.getItem(THEME_KEY);
      return t === "light" || t === "dark" ? t : null;
    } catch {
      return null;
    }
  };
  const apply = () => {
    const mode = stored() ?? (system.matches ? "dark" : "light");
    root.dataset.mode = mode;
    for (const b of document.querySelectorAll<HTMLButtonElement>("[data-theme-toggle]")) {
      const label = mode === "dark" ? "Switch to light mode" : "Switch to dark mode";
      b.setAttribute("aria-label", label);
      b.title = label;
    }
  };
  for (const b of document.querySelectorAll<HTMLButtonElement>("[data-theme-toggle]")) {
    b.addEventListener("click", () => {
      const next = root.dataset.mode === "dark" ? "light" : "dark";
      root.dataset.theme = next;
      try {
        localStorage.setItem(THEME_KEY, next);
      } catch {}
      apply();
    });
  }
  // Absent = System: follow the OS while nothing is stored, and other
  // tabs (or the app at /app) when they change the choice.
  system.addEventListener("change", apply);
  window.addEventListener("storage", (e) => {
    if (e.key === THEME_KEY || e.key === null) {
      const t = stored();
      if (t) root.dataset.theme = t;
      else delete root.dataset.theme;
      apply();
    }
  });
  apply();
}

function initCopy() {
  for (const button of document.querySelectorAll<HTMLButtonElement>("button[data-copy]")) {
    let timer: ReturnType<typeof setTimeout> | undefined;
    const label = button.textContent?.trim() || "Copy";
    button.addEventListener("click", async () => {
      const id = button.dataset.copyFrom;
      const text = id
        ? (document.getElementById(id)?.textContent ?? "")
        : (button.dataset.copy ?? "");
      try {
        await navigator.clipboard.writeText(text);
      } catch {
        return; // clipboard blocked; the text is on the page to select
      }
      button.dataset.state = "copied";
      button.textContent = "Copied";
      clearTimeout(timer);
      timer = setTimeout(() => {
        button.dataset.state = "";
        button.textContent = label;
      }, 1600);
    });
  }
}

function initTabs() {
  for (const list of document.querySelectorAll<HTMLElement>("[role=tablist]")) {
    const tabs = [...list.querySelectorAll<HTMLButtonElement>("[role=tab]")];
    const copy = list.parentElement?.querySelector<HTMLButtonElement>("[data-copy-from]");
    const select = (tab: HTMLButtonElement) => {
      for (const t of tabs) {
        const on = t === tab;
        const panel = document.getElementById(t.getAttribute("aria-controls") ?? "");
        t.setAttribute("aria-selected", String(on));
        t.tabIndex = on ? 0 : -1;
        if (panel) panel.hidden = !on;
        if (on && copy && panel) copy.dataset.copyFrom = panel.id;
      }
    };
    tabs.forEach((tab, i) => {
      tab.addEventListener("click", () => select(tab));
      tab.addEventListener("keydown", (e) => {
        const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
        if (!step) return;
        const next = tabs[(i + step + tabs.length) % tabs.length];
        select(next);
        next.focus();
      });
    });
  }
}

export function initSite() {
  initTheme();
  initCopy();
  initTabs();
  initHero();
}
