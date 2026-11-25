// Markdown tables from harness results (docs/perf.md).
//   bun scripts/perf/report.mjs perf/baseline.json [perf/after.json]
import { readFileSync } from "node:fs";

const [beforeFile, afterFile] = process.argv.slice(2);
const before = JSON.parse(readFileSync(beforeFile, "utf8"));
const after = afterFile ? JSON.parse(readFileSync(afterFile, "utf8")) : null;

const pct = (a, b) =>
  a === 0 ? (b === 0 ? "±0%" : "new") : `${b > a ? "+" : ""}${Math.round(((b - a) / a) * 100)}%`;
const cell = (a, b) => (b === undefined ? `${a}` : `${a} → ${b} (${pct(a, b)})`);
const top = (t, n = 5) =>
  t
    .slice(0, n)
    .map(([k, v]) => `${k} ${v}`)
    .join(", ") || "—";

const lines = [];
lines.push(
  after
    ? "| Scenario | Commits | Component renders | Long tasks (n / ms) | Scripting ms |"
    : "| Scenario | Commits | Component renders | Long tasks (n / ms) | Scripting ms | Top 5 |",
);
lines.push(after ? "| --- | --- | --- | --- | --- |" : "| --- | --- | --- | --- | --- | --- |");
for (const [id, s] of Object.entries(before.scenarios)) {
  const b = s.median;
  const a = after?.scenarios[id]?.median;
  const ready = b.readyMs !== undefined ? ` (first note ${cell(b.readyMs, a?.readyMs)} ms)` : "";
  const row = [
    `**${id}** ${s.name}${ready}`,
    cell(b.commits, a?.commits),
    cell(b.renders, a?.renders),
    a
      ? `${b.longTasks} / ${b.longTaskMs} → ${a.longTasks} / ${a.longTaskMs}`
      : `${b.longTasks} / ${b.longTaskMs}`,
    cell(b.scriptMs, a?.scriptMs),
  ];
  if (!after) row.push(top(s.top));
  lines.push(`| ${row.join(" | ")} |`);
}
console.log(lines.join("\n"));

console.log("\n#### Top 15 components by renders (median of runs)\n");
for (const [id, s] of Object.entries(before.scenarios)) {
  const a = after?.scenarios[id];
  console.log(`- **${id}** before: ${top(s.top, 15)}`);
  if (a) console.log(`  - after: ${top(a.top, 15)}`);
}
