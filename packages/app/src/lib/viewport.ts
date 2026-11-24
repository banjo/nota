import { useEffect, useSyncExternalStore } from "react";

// What a phone's viewport is doing, as CSS variables on <html>:
//   --visual-height   the visible height (shrinks with the keyboard)
//   --keyboard-inset  how much of the layout the keyboard covers
//   --app-height      set only while the keyboard is up, so the shell
//                     shrinks to the visible area on browsers that don't
//                     resize the layout for it (iOS Safari, WKWebView)
// Pinch zoom also shrinks the visual viewport; that is not a keyboard,
// so nothing changes while the page is zoomed.

/** The keyboard's height from the viewport sizes; 0 when zoomed or closed. */
export function keyboardInset(v: {
  innerHeight: number;
  height: number;
  offsetTop: number;
  scale: number;
}): number {
  if (Math.abs(v.scale - 1) > 0.01) return 0;
  const inset = v.innerHeight - v.height - v.offsetTop;
  // Below ~80px it is browser chrome moving, not a keyboard.
  return inset > 80 ? Math.round(inset) : 0;
}

/** Keeps the viewport variables up to date; mount once. */
export function useViewport() {
  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;
    const root = document.documentElement.style;
    let frame = 0;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const kb = keyboardInset({
          innerHeight: window.innerHeight,
          height: vv.height,
          offsetTop: vv.offsetTop,
          scale: vv.scale,
        });
        root.setProperty("--visual-height", `${Math.round(vv.height)}px`);
        root.setProperty("--keyboard-inset", `${kb}px`);
        if (kb) {
          root.setProperty("--app-height", `${Math.round(vv.height)}px`);
          // iOS pans the page up to show the caret; the shell already fits.
          if (window.scrollY) window.scrollTo(0, 0);
        } else root.removeProperty("--app-height");
        document.documentElement.toggleAttribute("data-keyboard", kb > 0);
      });
    };
    update();
    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    return () => {
      cancelAnimationFrame(frame);
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
    };
  }, []);
}

function useMedia(query: string): boolean {
  return useSyncExternalStore(
    (on) => {
      if (typeof matchMedia === "undefined") return () => {};
      const m = matchMedia(query);
      m.addEventListener("change", on);
      return () => m.removeEventListener("change", on);
    },
    () => typeof matchMedia !== "undefined" && matchMedia(query).matches,
    () => false,
  );
}

/** One pane at a time (the 720px navigation stack): menus become sheets. */
export const NARROW = "(max-width: 720px)";
export const useNarrow = () => useMedia(NARROW);
