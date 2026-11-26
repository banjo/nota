// A line diff for showing what changed between two versions of a note.

export type DiffLine = { kind: "same" | "add" | "del"; text: string };

/**
 * The lines of `a` and `b` as one list: lines only in `a` are "del", lines
 * only in `b` are "add". Myers' algorithm, after trimming the lines both
 * share at the start and the end, so a small edit to a long note is cheap.
 */
export function diffLines(a: string, b: string): DiffLine[] {
  const x = split(a);
  const y = split(b);
  let start = 0;
  while (start < x.length && start < y.length && x[start] === y[start]) start++;
  let endX = x.length;
  let endY = y.length;
  while (endX > start && endY > start && x[endX - 1] === y[endY - 1]) {
    endX--;
    endY--;
  }
  const out: DiffLine[] = x.slice(0, start).map((text) => ({ kind: "same", text }));
  out.push(...myers(x.slice(start, endX), y.slice(start, endY)));
  out.push(...x.slice(endX).map((text): DiffLine => ({ kind: "same", text })));
  return out;
}

function split(s: string): string[] {
  if (s === "") return [];
  const lines = s.split("\n");
  if (lines[lines.length - 1] === "") lines.pop();
  return lines;
}

function myers(a: string[], b: string[]): DiffLine[] {
  const n = a.length;
  const m = b.length;
  if (n === 0) return b.map((text) => ({ kind: "add", text }));
  if (m === 0) return a.map((text) => ({ kind: "del", text }));
  const max = n + m;
  const off = max;
  let v = new Int32Array(2 * max + 2);
  const trace: Int32Array[] = [];
  outer: for (let d = 0; d <= max; d++) {
    trace.push(v.slice());
    const next = v;
    for (let k = -d; k <= d; k += 2) {
      let i =
        k === -d || (k !== d && next[off + k - 1]! < next[off + k + 1]!)
          ? next[off + k + 1]!
          : next[off + k - 1]! + 1;
      let j = i - k;
      while (i < n && j < m && a[i] === b[j]) {
        i++;
        j++;
      }
      next[off + k] = i;
      if (i >= n && j >= m) {
        trace.push(next.slice());
        break outer;
      }
    }
    v = next;
  }
  // Walk back through the trace from the end.
  const out: DiffLine[] = [];
  let i = n;
  let j = m;
  for (let d = trace.length - 2; d >= 0 && (i > 0 || j > 0); d--) {
    const prev = trace[d]!;
    const k = i - j;
    const down = k === -d || (k !== d && prev[off + k - 1]! < prev[off + k + 1]!);
    const pk = down ? k + 1 : k - 1;
    const pi = prev[off + pk]!;
    const pj = pi - pk;
    while (i > pi && j > pj) {
      j--;
      out.push({ kind: "same", text: a[--i]! });
    }
    if (d === 0) break;
    if (down) out.push({ kind: "add", text: b[--j]! });
    else out.push({ kind: "del", text: a[--i]! });
  }
  while (i > 0 && j > 0) {
    j--;
    out.push({ kind: "same", text: a[--i]! });
  }
  return out.reverse();
}

/** How many lines were added and removed. */
export function diffStats(lines: DiffLine[]): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const l of lines) {
    if (l.kind === "add") added++;
    else if (l.kind === "del") removed++;
  }
  return { added, removed };
}
