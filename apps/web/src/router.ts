// The web app's own tiny router. The desktop app has no URLs, so this
// lives in apps/web: /app/ (notes), /app/signup, /app/connect and
// /app/devices. Anything else under /app is a deep link into the app.

import { useSyncExternalStore } from "react";

export const BASE = "/app";

const listeners = new Set<() => void>();

function emit() {
  for (const l of listeners) l();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  window.addEventListener("popstate", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("popstate", listener);
  };
}

/** The path inside /app, always starting with a slash. */
export function readPath(): string {
  let p = window.location.pathname;
  if (p === BASE) return "/";
  if (p.startsWith(`${BASE}/`)) p = p.slice(BASE.length);
  return p.startsWith("/") ? p : `/${p}`;
}

export function usePath(): string {
  return useSyncExternalStore(subscribe, readPath, () => "/");
}

export function href(to: string): string {
  return `${BASE}${to === "/" ? "/" : to}`;
}

export function navigate(to: string, replace = false): void {
  const url = href(to) + window.location.search;
  if (replace) window.history.replaceState(null, "", url);
  else window.history.pushState(null, "", url);
  emit();
  window.scrollTo(0, 0);
}
