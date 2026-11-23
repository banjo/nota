// The terminal side of the hero: one Markdown file seen from vim, then
// from git. Shared by the prerendered page (the last frame is in the
// HTML, so the page reads the same with JavaScript off or reduced motion)
// and by the animation in hero.ts.

export const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;");

export const prompt = (cmd: string, caret = true) =>
  `<span class="prompt">~/Notes $</span> ${esc(cmd)}${caret ? '<span class="tcaret"></span>' : ""}`;

/** The note as vim shows it; `extra` is the line being typed. */
export const file = (extra = "", status = "draft", mia = " ") =>
  [
    `<span class="dim">---</span>`,
    `<span class="key">tags:</span> [acme, meeting]`,
    `<span class="key">status:</span> ${status}`,
    `<span class="dim">---</span>`,
    `<span class="h"># Weekly sync</span>`,
    ``,
    `<span class="h">## Decisions</span>`,
    `- Ship the beta on the 14th`,
    `- Keep the free tier`,
    ``,
    `<span class="h">## Todo</span>`,
    `- [${mia}] Mia: draft release notes`,
    extra,
  ].join("\n");

/** The end of the story: both edits, as git sees them. */
export const diff = () =>
  [
    `<span class="prompt">~/Notes $</span> git diff -U0`,
    `<span class="h">diff --git a/Acme/Weekly sync.md b/Acme/Weekly sync.md</span>`,
    `<span class="dim">@@ -3 +3 @@</span>`,
    `<span class="del">-status: draft</span>`,
    `<span class="add">+status: done</span>`,
    `<span class="dim">@@ -12 +12,2 @@</span>`,
    `<span class="del">-- [ ] Mia: draft release notes</span>`,
    `<span class="add">+- [x] Mia: draft release notes</span>`,
    `<span class="add">+- [ ] Sam: book the venue</span>`,
    ``,
    prompt(""),
  ].join("\n");
