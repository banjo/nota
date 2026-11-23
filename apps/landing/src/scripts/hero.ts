// The hero: a line typed in vim shows up in Nota, a box ticked in Nota
// shows up in git. A script of sleeps over the prerendered last frame;
// nothing runs with reduced motion, while the tab is hidden or while
// the hero is off screen.

import { diff, esc, file, prompt } from "./frames.ts";

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms));

export function initHero() {
  const root = document.querySelector<HTMLElement>("[data-hero]");
  if (!root || matchMedia("(prefers-reduced-motion: reduce)").matches) return;

  const $ = (name: string) => root.querySelector<HTMLElement>(`[data-h="${name}"]`)!;
  const screen = $("screen");
  const sam = $("sam");
  const mia = $("mia");
  const box = $("box");
  const status = $("status");
  const statusText = $("status-text");
  const sync = $("sync");
  const syncText = $("sync-text");
  const pointer = $("pointer");
  const pane = $("app");

  let visible = true;
  new IntersectionObserver(([e]) => (visible = e.isIntersecting)).observe(root);
  const ready = async () => {
    while (!visible || document.hidden) await sleep(300);
  };

  const draw = (html: string) => (screen.innerHTML = html);

  const typeCmd = async (cmd: string) => {
    for (let i = 1; i <= cmd.length; i++) {
      draw(prompt(cmd.slice(0, i)));
      await sleep(32 + Math.random() * 40);
    }
  };

  const pulse = async (text: string) => {
    sync.dataset.on = "";
    syncText.textContent = text;
    await sleep(1400);
    delete sync.dataset.on;
    syncText.textContent = "Up to date";
  };

  const reset = () => {
    sam.dataset.hidden = "";
    delete mia.dataset.checked;
    delete status.dataset.done;
    statusText.textContent = "Draft";
    delete sync.dataset.on;
    syncText.textContent = "Up to date";
    pointer.style.opacity = "0";
  };

  const scene = async () => {
    reset();
    draw(prompt(""));
    await sleep(600);
    await typeCmd('nvim "Acme/Weekly sync.md"');
    await sleep(350);

    // vim opens with the cursor on a new line under the todos
    const vim = (l: string, mode = "-- INSERT --") =>
      file(`<span class="vimline">${esc(l)}<span class="tcaret"></span></span>`) +
      `\n\n<span class="dim">${mode}</span>`;
    draw(vim("", ""));
    await sleep(500);
    let line = "";
    for (const ch of "- [ ] Sam: book the venue") {
      line += ch;
      draw(vim(line));
      await sleep(38 + Math.random() * 40);
    }
    await sleep(300);
    const typed = file(esc(line));
    for (const s of [":", ":w", ":wq"]) {
      draw(typed + `\n\n${s}<span class="tcaret"></span>`);
      await sleep(160);
    }
    await sleep(200);
    draw(prompt(""));

    // Nota sees the file change on disk
    delete sam.dataset.hidden;
    sam.dataset.flash = "";
    void pulse("Changed on disk");
    await sleep(60);
    delete sam.dataset.flash;
    await sleep(1300);

    // and the other way round: tick a box in the app
    const p = pane.getBoundingClientRect();
    const b = box.getBoundingClientRect();
    pointer.style.transition = "none";
    pointer.style.transform = `translate(${p.width - 80}px, ${p.height - 40}px)`;
    pointer.getBoundingClientRect();
    pointer.style.transition = "";
    pointer.style.opacity = "1";
    pointer.style.transform = `translate(${b.left - p.left + 6}px, ${b.top - p.top + 6}px)`;
    await sleep(800);
    box.dataset.press = "";
    await sleep(120);
    delete box.dataset.press;
    mia.dataset.checked = "";
    await sleep(500);
    status.dataset.done = "";
    statusText.textContent = "Done";
    void pulse("Saved");
    await sleep(400);
    pointer.style.opacity = "0";
    await sleep(500);
    await typeCmd("git diff -U0");
    await sleep(300);
    draw(diff());
  };

  void (async () => {
    // The last frame is already on the page: let it be read first.
    await sleep(2600);
    for (;;) {
      await ready();
      await scene();
      await sleep(4200);
    }
  })();
}
