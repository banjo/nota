// Injected before any page script (Playwright addInitScript). A stub React
// DevTools hook: React calls onCommitFiberRoot after every commit, and we
// count, per component name, the components that actually rendered in it.
//
// "Rendered" = the fiber carries React's PerformedWork flag (1) and sits in
// the part of the tree React reconciled this commit. A subtree React bailed
// out of keeps its old fibers (fiber.child === fiber.alternate.child), whose
// flags are stale from an earlier render, so it is skipped. A component
// whose function ran but bailed out (same props, same state) does not set
// PerformedWork; React DevTools' profiler counts the same way.
(() => {
  const PERFORMED_WORK = 1;
  // FunctionComponent, ClassComponent, ForwardRef, SimpleMemoComponent.
  // MemoComponent (14) is a wrapper whose inner component has its own fiber.
  const COMPONENT = new Set([0, 1, 11, 15]);

  const nameOf = (f) => {
    const t = f.type;
    if (!t) return "Anonymous";
    if (typeof t === "function") return t.displayName || t.name || "Anonymous";
    if (f.tag === 11)
      return t.displayName || t.render?.displayName || t.render?.name || "ForwardRef";
    if (f.tag === 15) return t.displayName || t.name || "Memo";
    return "Anonymous";
  };

  const state = { commits: 0, renders: 0, byName: Object.create(null), longTasks: [], trace: [] };
  // --trace: per commit, what rendered (for finding causes, not for the numbers).
  let commitNames = null;

  function walk(root) {
    let n = 0;
    const stack = [root];
    while (stack.length) {
      const f = stack.pop();
      if (COMPONENT.has(f.tag) && f.flags & PERFORMED_WORK) {
        const name = nameOf(f);
        state.byName[name] = (state.byName[name] || 0) + 1;
        if (commitNames) commitNames[name] = (commitNames[name] || 0) + 1;
        n++;
      }
      const alt = f.alternate;
      if (f.child && !(alt && alt.child === f.child))
        for (let c = f.child; c; c = c.sibling) stack.push(c);
    }
    return n;
  }

  let id = 0;
  window.__REACT_DEVTOOLS_GLOBAL_HOOK__ = {
    renderers: new Map(),
    supportsFiber: true,
    isDisabled: false,
    inject(internals) {
      id++;
      this.renderers.set(id, internals);
      return id;
    },
    checkDCE() {},
    onScheduleFiberRoot() {},
    onCommitFiberUnmount() {},
    onPostCommitFiberRoot() {},
    onCommitFiberRoot(_id, root) {
      state.commits++;
      if (window.__perfTraceOn) commitNames = Object.create(null);
      const n = walk(root.current);
      state.renders += n;
      if (commitNames) {
        const top = Object.entries(commitNames)
          .sort((a, b) => b[1] - a[1])
          .slice(0, 8)
          .map(([k, v]) => `${k}:${v}`);
        state.trace.push({ t: Math.round(performance.now()), n, top: top.join(" ") });
        commitNames = null;
      }
    },
  };

  try {
    new PerformanceObserver((list) => {
      for (const e of list.getEntries())
        state.longTasks.push({ start: e.startTime, duration: e.duration });
    }).observe({ type: "longtask", buffered: true });
  } catch {}

  window.__perf = {
    reset() {
      state.commits = 0;
      state.renders = 0;
      state.byName = Object.create(null);
      state.longTasks = [];
      state.trace = [];
    },
    read() {
      return JSON.parse(JSON.stringify(state));
    },
    lastCommitAt: 0,
  };
  const orig = window.__REACT_DEVTOOLS_GLOBAL_HOOK__.onCommitFiberRoot;
  window.__REACT_DEVTOOLS_GLOBAL_HOOK__.onCommitFiberRoot = function (...a) {
    window.__perf.lastCommitAt = performance.now();
    return orig.apply(this, a);
  };
})();
