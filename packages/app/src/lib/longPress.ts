import { type MouseEvent, type PointerEvent, useEffect, useRef } from "react";

/** How long a finger rests before a press becomes a long press (iOS uses ~500ms). */
export const LONG_PRESS_MS = 450;
/** A finger that moves this far is scrolling, not pressing. */
const SLOP = 10;

/**
 * Long-press for touch: after LONG_PRESS_MS without moving, `onLongPress`
 * runs and the click that follows the lift is swallowed. Mouse and pen
 * are left alone, so a desk keeps its plain clicks (and right-click).
 */
export function useLongPress(onLongPress: () => void) {
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const start = useRef<{ x: number; y: number } | null>(null);
  const fired = useRef(false);
  const cancel = () => {
    clearTimeout(timer.current);
    timer.current = undefined;
    start.current = null;
  };
  useEffect(() => cancel, []);
  return {
    onPointerDown(e: PointerEvent) {
      if (e.pointerType !== "touch" || !e.isPrimary) return;
      fired.current = false;
      start.current = { x: e.clientX, y: e.clientY };
      clearTimeout(timer.current);
      timer.current = setTimeout(() => {
        timer.current = undefined;
        fired.current = true;
        navigator.vibrate?.(8);
        onLongPress();
      }, LONG_PRESS_MS);
    },
    onPointerMove(e: PointerEvent) {
      const s = start.current;
      if (!s || !timer.current) return;
      if (Math.hypot(e.clientX - s.x, e.clientY - s.y) > SLOP) cancel();
    },
    onPointerUp: cancel,
    onPointerCancel: cancel,
    onContextMenu(e: MouseEvent) {
      // Android raises the context menu for the same press.
      if (fired.current || timer.current) e.preventDefault();
    },
    onClickCapture(e: MouseEvent) {
      if (!fired.current) return;
      fired.current = false;
      e.preventDefault();
      e.stopPropagation();
    },
  };
}
