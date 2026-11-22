// Whether the API answered last time. The HTTP clients report each
// request's outcome; the banner shows while it is down.

type Listener = (up: boolean) => void;

let up = true;
const listeners = new Set<Listener>();

export const connection = {
  get: () => up,
  set(value: boolean) {
    if (value === up) return;
    up = value;
    for (const l of listeners) l(up);
  },
  subscribe(listener: Listener): () => void {
    listeners.add(listener);
    listener(up);
    return () => listeners.delete(listener);
  },
};
