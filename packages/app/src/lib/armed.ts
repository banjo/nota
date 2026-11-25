import { useLayoutEffect, useRef, useState } from "react";
import { flushSync } from "react-dom";

/**
 * A heavy control that repeats on every row (a row's "…" menu, a hover
 * button's tooltip) mounts the first time the pointer reaches it or it
 * gets focus. Until then the row renders a stand-in: the same button,
 * without the Radix machinery, which costs a dozen components and a
 * layout pass per row when a folder of notes opens.
 *
 * Spread `standIn` on the stand-in button and pass `ref` to the real
 * one. Once armed it stays.
 * - Pointer enter arms it synchronously: React would otherwise render a
 *   pointer-enter update at a lower priority than a press, and a press
 *   that comes right after (a quick flick, a script) could land on the
 *   stand-in or see the button swapped between down and up.
 * - Focus (Tab) arms it, and the real button takes the focus over
 *   before paint.
 */
export function useArmed<T extends HTMLElement = HTMLButtonElement>() {
  const [armed, setArmed] = useState(false);
  const refocus = useRef(false);
  const ref = useRef<T>(null);
  useLayoutEffect(() => {
    if (!armed || !refocus.current) return;
    refocus.current = false;
    ref.current?.focus();
  }, [armed]);
  const standIn = {
    onPointerEnter: () => flushSync(() => setArmed(true)),
    onFocus: () => {
      refocus.current = true;
      setArmed(true);
    },
  };
  return { armed, ref, standIn };
}
